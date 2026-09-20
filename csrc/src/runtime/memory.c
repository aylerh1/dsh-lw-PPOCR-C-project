#include "session_internal.h"

/*
 * Lifetime-aware workspace planner.
 * Intermediate tensors whose lifetimes do not overlap may reuse the same byte
 * range. This keeps inference allocation-free after session creation while the
 * configured workspace ceiling remains a hard limit.
 */
#include "lwm_read.h"

#include <stdlib.h>

typedef struct free_block {
    uint64_t offset;
    uint64_t size;
} free_block;

static uint64_t align_workspace(uint64_t value) {
    return (value + (LW_WORKSPACE_ALIGNMENT - 1u)) & ~(uint64_t)(LW_WORKSPACE_ALIGNMENT - 1u);
}

static int add_free_block(free_block* blocks, uint32_t* count, uint32_t capacity, uint64_t offset,
                          uint64_t size) {
    uint32_t position = 0u;
    uint32_t i;
    if (*count >= capacity) {
        return 0;
    }
    while (position < *count && blocks[position].offset < offset) {
        ++position;
    }
    for (i = *count; i > position; --i) {
        blocks[i] = blocks[i - 1u];
    }
    blocks[position].offset = offset;
    blocks[position].size = size;
    ++(*count);
    if (position > 0u &&
        blocks[position - 1u].offset + blocks[position - 1u].size == blocks[position].offset) {
        blocks[position - 1u].size += blocks[position].size;
        for (i = position; i + 1u < *count; ++i) {
            blocks[i] = blocks[i + 1u];
        }
        --(*count);
        --position;
    }
    if (position + 1u < *count &&
        blocks[position].offset + blocks[position].size == blocks[position + 1u].offset) {
        blocks[position].size += blocks[position + 1u].size;
        for (i = position + 1u; i + 1u < *count; ++i) {
            blocks[i] = blocks[i + 1u];
        }
        --(*count);
    }
    return 1;
}

static int allocate_block(free_block* blocks, uint32_t* count, uint64_t size, uint64_t* offset) {
    uint32_t i;
    uint32_t j;
    for (i = 0u; i < *count; ++i) {
        if (blocks[i].size >= size) {
            *offset = blocks[i].offset;
            blocks[i].offset += size;
            blocks[i].size -= size;
            if (blocks[i].size == 0u) {
                for (j = i; j + 1u < *count; ++j) {
                    blocks[j] = blocks[j + 1u];
                }
                --(*count);
            }
            return 1;
        }
    }
    return 0;
}

static lw_status lw_plan_workspace_v1(lw_session* session, uint64_t max_workspace_size,
                                      lw_error* error) {
    const lw_model* model = session->model;
    free_block* blocks;
    uint32_t free_count = 0u;
    uint64_t workspace_end = 0u;
    uint32_t i;
    uint32_t tensor_index;

    blocks = (free_block*)calloc((size_t)model->info.tensor_count + 1u, sizeof(*blocks));
    if (blocks == NULL) {
        lw_set_error(error, LW_STATUS_OUT_OF_MEMORY, "unable to allocate workspace planner state");
        return LW_STATUS_OUT_OF_MEMORY;
    }
    for (tensor_index = 0u; tensor_index < model->info.tensor_count; ++tensor_index) {
        lw_runtime_tensor* tensor = &session->tensors[tensor_index];
        tensor->workspace_offset = UINT64_MAX;
        tensor->birth_node = -1;
        tensor->last_use_node = -1;
        tensor->workspace_live = 0;
    }
    /* First determine when each tensor is born and when its final consumer
     * runs. Graph outputs stay live until execution has completely finished. */
    for (i = 0u; i < model->info.node_count; ++i) {
        const uint8_t* node =
            model->bytes + (size_t)model->node_offset + (size_t)i * LWM_V0_NODE_SIZE;
        uint16_t input_count = lwm_read_u16(node + 2);
        uint16_t output_count = lwm_read_u16(node + 4);
        uint32_t j;
        for (j = 0u; j < input_count; ++j) {
            lw_runtime_tensor* tensor = &session->tensors[lwm_read_u32(node + 8u + j * 4u)];
            tensor->last_use_node = (int32_t)i;
        }
        for (j = 0u; j < output_count; ++j) {
            lw_runtime_tensor* tensor = &session->tensors[lwm_read_u32(node + 40u + j * 4u)];
            if (tensor->birth_node != -1) {
                free(blocks);
                lw_set_error(error, LW_STATUS_INVALID_FORMAT,
                             "multiple nodes produce the same tensor");
                return LW_STATUS_INVALID_FORMAT;
            }
            tensor->birth_node = (int32_t)i;
            tensor->last_use_node = (int32_t)i;
        }
    }
    for (i = 0u; i < model->info.output_count; ++i) {
        uint32_t index = lwm_read_u32(model->bytes + (size_t)model->output_offset + (size_t)i * 4u);
        session->tensors[index].last_use_node = (int32_t)model->info.node_count;
    }

    /* Walking nodes in execution order lets an expired input range be reused
     * immediately by a later output instead of growing the workspace. */
    for (i = 0u; i < model->info.node_count; ++i) {
        const uint8_t* node =
            model->bytes + (size_t)model->node_offset + (size_t)i * LWM_V0_NODE_SIZE;
        uint16_t output_count = lwm_read_u16(node + 4);
        uint32_t j;
        for (tensor_index = 0u; tensor_index < model->info.tensor_count; ++tensor_index) {
            lw_runtime_tensor* tensor = &session->tensors[tensor_index];
            if (tensor->workspace_live && tensor->last_use_node < (int32_t)i) {
                uint64_t size = align_workspace(tensor->byte_size);
                if (!add_free_block(blocks, &free_count, model->info.tensor_count + 1u,
                                    tensor->workspace_offset, size)) {
                    free(blocks);
                    lw_set_error(error, LW_STATUS_OUT_OF_MEMORY,
                                 "workspace free-list capacity exceeded");
                    return LW_STATUS_OUT_OF_MEMORY;
                }
                tensor->workspace_live = 0;
            }
        }
        for (j = 0u; j < output_count; ++j) {
            uint32_t output_index = lwm_read_u32(node + 40u + j * 4u);
            lw_runtime_tensor* tensor = &session->tensors[output_index];
            uint64_t size;
            if (lw_execution_plan_is_skipped_tensor(session, output_index)) {
                continue;
            }
            uint64_t offset;
            if (tensor->byte_size > UINT64_MAX - (LW_WORKSPACE_ALIGNMENT - 1u)) {
                free(blocks);
                lw_set_error(error, LW_STATUS_MEMORY_LIMIT,
                             "aligned tensor workspace size overflows");
                return LW_STATUS_MEMORY_LIMIT;
            }
            size = align_workspace(tensor->byte_size);
            if (!allocate_block(blocks, &free_count, size, &offset)) {
                if (workspace_end > UINT64_MAX - size) {
                    free(blocks);
                    lw_set_error(error, LW_STATUS_MEMORY_LIMIT, "workspace size overflows");
                    return LW_STATUS_MEMORY_LIMIT;
                }
                offset = workspace_end;
                workspace_end += size;
            }
            if (workspace_end > max_workspace_size || workspace_end > SIZE_MAX) {
                free(blocks);
                lw_set_error(error, LW_STATUS_MEMORY_LIMIT,
                             "planned workspace exceeds max_workspace_size");
                return LW_STATUS_MEMORY_LIMIT;
            }
            tensor->workspace_offset = offset;
            tensor->workspace_live = 1;
        }
    }
    free(blocks);
    session->workspace_bytes = (size_t)workspace_end;
    return LW_STATUS_OK;
}

#if defined(LW_EXPERIMENTAL_WORKSPACE_PLANNER_V2)
typedef struct workspace_interval {
    uint32_t tensor_index;
    uint64_t size;
    uint64_t offset;
    int32_t birth_node;
    int32_t last_use_node;
    int placed;
} workspace_interval;

static int interval_lifetimes_overlap(const workspace_interval* left,
                                       const workspace_interval* right) {
    return left->birth_node <= right->last_use_node &&
           right->birth_node <= left->last_use_node;
}

static int interval_before(const workspace_interval* left, const workspace_interval* right) {
    if (left->size != right->size) {
        return left->size > right->size;
    }
    if (left->birth_node != right->birth_node) {
        return left->birth_node < right->birth_node;
    }
    return left->tensor_index < right->tensor_index;
}

static int align_workspace_checked(uint64_t value, uint64_t* aligned) {
    if (value > UINT64_MAX - (LW_WORKSPACE_ALIGNMENT - 1u)) {
        return 0;
    }
    *aligned = align_workspace(value);
    return 1;
}

static int interval_find_offset(const workspace_interval* intervals, uint32_t count,
                                uint32_t current, uint64_t* out_offset) {
    uint64_t candidate = 0u;
    for (;;) {
        uint32_t i;
        int moved = 0;
        uint64_t candidate_end;
        if (intervals[current].size > UINT64_MAX - candidate) {
            return 0;
        }
        candidate_end = candidate + intervals[current].size;
        for (i = 0u; i < count; ++i) {
            const workspace_interval* other = &intervals[i];
            uint64_t other_end;
            if (!other->placed || !interval_lifetimes_overlap(&intervals[current], other)) {
                continue;
            }
            if (other->size > UINT64_MAX - other->offset) {
                return 0;
            }
            other_end = other->offset + other->size;
            if (candidate < other_end && other->offset < candidate_end) {
                if (!align_workspace_checked(other_end, &candidate)) {
                    return 0;
                }
                moved = 1;
                break;
            }
        }
        if (!moved) {
            *out_offset = candidate;
            return 1;
        }
    }
}

static lw_status lw_plan_workspace_v2(lw_session* session, uint64_t max_workspace_size,
                                      lw_error* error) {
    const lw_model* model = session->model;
    workspace_interval* intervals;
    uint32_t interval_count = 0u;
    uint32_t i;
    uint64_t workspace_end = 0u;

    intervals = (workspace_interval*)calloc((size_t)model->info.tensor_count,
                                             sizeof(*intervals));
    if (intervals == NULL && model->info.tensor_count != 0u) {
        lw_set_error(error, LW_STATUS_OUT_OF_MEMORY,
                     "unable to allocate v2 workspace planner state");
        return LW_STATUS_OUT_OF_MEMORY;
    }
    for (i = 0u; i < model->info.tensor_count; ++i) {
        lw_runtime_tensor* tensor = &session->tensors[i];
        tensor->workspace_offset = UINT64_MAX;
        tensor->birth_node = -1;
        tensor->last_use_node = -1;
        tensor->workspace_live = 0;
    }
    /* Recompute the semantic lifetimes independently of the v1 planner. */
    for (i = 0u; i < model->info.node_count; ++i) {
        const uint8_t* node =
            model->bytes + (size_t)model->node_offset + (size_t)i * LWM_V0_NODE_SIZE;
        uint16_t input_count = lwm_read_u16(node + 2u);
        uint16_t output_count = lwm_read_u16(node + 4u);
        uint32_t j;
        for (j = 0u; j < input_count; ++j) {
            uint32_t index = lwm_read_u32(node + 8u + (size_t)j * sizeof(uint32_t));
            session->tensors[index].last_use_node = (int32_t)i;
        }
        for (j = 0u; j < output_count; ++j) {
            uint32_t index = lwm_read_u32(node + 40u + (size_t)j * sizeof(uint32_t));
            lw_runtime_tensor* tensor = &session->tensors[index];
            if (tensor->birth_node != -1) {
                free(intervals);
                lw_set_error(error, LW_STATUS_INVALID_FORMAT,
                             "multiple nodes produce the same tensor");
                return LW_STATUS_INVALID_FORMAT;
            }
            tensor->birth_node = (int32_t)i;
            tensor->last_use_node = (int32_t)i;
            if (lw_execution_plan_is_skipped_tensor(session, index)) {
                continue;
            }
            intervals[interval_count].tensor_index = index;
            intervals[interval_count].birth_node = (int32_t)i;
            intervals[interval_count].last_use_node = (int32_t)i;
            if (!align_workspace_checked(tensor->byte_size, &intervals[interval_count].size)) {
                free(intervals);
                lw_set_error(error, LW_STATUS_MEMORY_LIMIT,
                             "aligned tensor workspace size overflows");
                return LW_STATUS_MEMORY_LIMIT;
            }
            ++interval_count;
        }
    }
    for (i = 0u; i < model->info.output_count; ++i) {
        uint32_t index = lwm_read_u32(model->bytes + (size_t)model->output_offset +
                                      (size_t)i * sizeof(uint32_t));
        session->tensors[index].last_use_node = (int32_t)model->info.node_count;
    }
    for (i = 0u; i < interval_count; ++i) {
        intervals[i].last_use_node = session->tensors[intervals[i].tensor_index].last_use_node;
    }
    /* Stable insertion sort keeps the plan deterministic across compilers. */
    for (i = 1u; i < interval_count; ++i) {
        workspace_interval value = intervals[i];
        uint32_t position = i;
        while (position > 0u && interval_before(&value, &intervals[position - 1u])) {
            intervals[position] = intervals[position - 1u];
            --position;
        }
        intervals[position] = value;
    }
    for (i = 0u; i < interval_count; ++i) {
        workspace_interval* interval = &intervals[i];
        uint64_t end;
        if (interval->size == 0u) {
            interval->offset = 0u;
        } else if (!interval_find_offset(intervals, interval_count, i, &interval->offset) ||
                   interval->size > UINT64_MAX - interval->offset) {
            free(intervals);
            lw_set_error(error, LW_STATUS_MEMORY_LIMIT,
                         "v2 workspace interval placement overflows");
            return LW_STATUS_MEMORY_LIMIT;
        }
        interval->placed = 1;
        end = interval->offset + interval->size;
        if (end > workspace_end) {
            workspace_end = end;
        }
    }
    if (workspace_end > max_workspace_size || workspace_end > SIZE_MAX) {
        free(intervals);
        lw_set_error(error, LW_STATUS_MEMORY_LIMIT,
                     "planned v2 workspace exceeds max_workspace_size");
        return LW_STATUS_MEMORY_LIMIT;
    }
    for (i = 0u; i < interval_count; ++i) {
        lw_runtime_tensor* tensor = &session->tensors[intervals[i].tensor_index];
        tensor->workspace_offset = intervals[i].offset;
        tensor->workspace_live = intervals[i].size != 0u;
    }
    free(intervals);
    session->workspace_bytes = (size_t)workspace_end;
    return LW_STATUS_OK;
}
#endif

lw_status lw_plan_workspace(lw_session* session, uint64_t max_workspace_size, lw_error* error) {
#if defined(LW_EXPERIMENTAL_WORKSPACE_PLANNER_V2)
    const uint32_t tensor_count = session->model->info.tensor_count;
    uint64_t* v1_offsets =
        (uint64_t*)calloc((size_t)tensor_count, sizeof(*v1_offsets));
    unsigned char* v1_live =
        (unsigned char*)calloc((size_t)tensor_count, sizeof(*v1_live));
    uint64_t v1_workspace = 0u;
    lw_error v1_error;
    lw_error v2_error;
    lw_status v1_status;
    lw_status v2_status;
    uint32_t i;
    if ((v1_offsets == NULL || v1_live == NULL) && tensor_count != 0u) {
        free(v1_offsets);
        free(v1_live);
        lw_set_error(error, LW_STATUS_OUT_OF_MEMORY,
                     "unable to allocate workspace planner comparison state");
        return LW_STATUS_OUT_OF_MEMORY;
    }
    lw_error_init(&v1_error);
    v1_status = lw_plan_workspace_v1(session, max_workspace_size, &v1_error);
    if (v1_status == LW_STATUS_OK) {
        v1_workspace = (uint64_t)session->workspace_bytes;
        for (i = 0u; i < tensor_count; ++i) {
            v1_offsets[i] = session->tensors[i].workspace_offset;
            v1_live[i] = (unsigned char)(session->tensors[i].workspace_live != 0);
        }
    }
    lw_error_init(&v2_error);
    v2_status = lw_plan_workspace_v2(session, max_workspace_size, &v2_error);
    if (v2_status == LW_STATUS_OK &&
        (v1_status != LW_STATUS_OK || (uint64_t)session->workspace_bytes <= v1_workspace)) {
        free(v1_offsets);
        free(v1_live);
        if (error != NULL) {
            *error = v2_error;
        }
        return LW_STATUS_OK;
    }
    if (v1_status == LW_STATUS_OK) {
        session->workspace_bytes = (size_t)v1_workspace;
        for (i = 0u; i < tensor_count; ++i) {
            session->tensors[i].workspace_offset = v1_offsets[i];
            session->tensors[i].workspace_live = v1_live[i] != 0u;
        }
        free(v1_offsets);
        free(v1_live);
        if (error != NULL) {
            *error = v1_error;
        }
        return LW_STATUS_OK;
    }
    free(v1_offsets);
    free(v1_live);
    if (error != NULL) {
        *error = v2_status == LW_STATUS_OK ? v2_error : v1_error;
    }
    return v2_status == LW_STATUS_OK ? LW_STATUS_OK : v1_status;
#else
    return lw_plan_workspace_v1(session, max_workspace_size, error);
#endif
}
