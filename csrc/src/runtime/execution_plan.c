#include "session_internal.h"

#include "lwm_read.h"

#include <stdlib.h>
#include <string.h>

enum {
    LW_PLAN_OP_ADD = 2,
    LW_PLAN_OP_MUL = 3,
    LW_PLAN_OP_DIV = 4,
    LW_PLAN_OP_ERF = 5,
    LW_PLAN_OP_SOFTMAX = 15
};

static int constant_scalar_f32(const lw_session* session, uint32_t tensor_index, float expected) {
    const lw_runtime_tensor* tensor = &session->tensors[tensor_index];
    const uint8_t* disk;
    uint64_t data_offset;
    float value;
    if (tensor->dtype != LW_DTYPE_F32 || tensor->byte_size != sizeof(float) ||
        (tensor->flags & LWM_V0_TENSOR_FLAG_CONSTANT) == 0u) {
        return 0;
    }
    disk = session->model->bytes + (size_t)session->model->tensor_offset +
           (size_t)tensor_index * LWM_V0_TENSOR_SIZE;
    data_offset = lwm_read_u64(disk + 48u);
    memcpy(&value, session->model->bytes + (size_t)data_offset, sizeof(value));
    return value == expected;
}

int lw_match_fused_gelu(const lw_session* session, uint32_t node_index,
                        lw_fused_gelu_match* match) {
    const lw_model* model;
    uint32_t index;
    const lw_runtime_tensor* source;
    const lw_runtime_tensor* output;
    if (session == NULL || match == NULL || session->model == NULL) {
        return 0;
    }
    model = session->model;
    if (node_index > model->info.node_count || model->info.node_count - node_index < 5u) {
        return 0;
    }
    for (index = 0u; index < 5u; ++index) {
        const uint8_t* node = model->bytes + (size_t)model->node_offset +
                              (size_t)(node_index + index) * LWM_V0_NODE_SIZE;
        if (lwm_read_u16(node + 2u) != (index == 1u ? 1u : 2u) ||
            lwm_read_u16(node + 4u) != 1u) {
            return 0;
        }
        match->inputs[index][0] = lwm_read_u32(node + 8u);
        match->inputs[index][1] = index == 1u ? UINT32_MAX : lwm_read_u32(node + 12u);
        match->outputs[index] = lwm_read_u32(node + 40u);
    }
    {
        const uint8_t* node0 = model->bytes + (size_t)model->node_offset +
                               (size_t)node_index * LWM_V0_NODE_SIZE;
        const uint8_t* node1 = node0 + LWM_V0_NODE_SIZE;
        const uint8_t* node2 = node1 + LWM_V0_NODE_SIZE;
        const uint8_t* node3 = node2 + LWM_V0_NODE_SIZE;
        const uint8_t* node4 = node3 + LWM_V0_NODE_SIZE;
        if (lwm_read_u16(node0) != LW_PLAN_OP_DIV || lwm_read_u16(node1) != LW_PLAN_OP_ERF ||
            lwm_read_u16(node2) != LW_PLAN_OP_ADD || lwm_read_u16(node3) != LW_PLAN_OP_MUL ||
            lwm_read_u16(node4) != LW_PLAN_OP_MUL ||
            match->inputs[1][0] != match->outputs[0] ||
            (match->inputs[2][0] != match->outputs[1] &&
             match->inputs[2][1] != match->outputs[1]) ||
            (match->inputs[3][0] != match->inputs[0][0] &&
             match->inputs[3][1] != match->inputs[0][0]) ||
            (match->inputs[3][0] != match->outputs[2] &&
             match->inputs[3][1] != match->outputs[2]) ||
            (match->inputs[4][0] != match->outputs[3] &&
             match->inputs[4][1] != match->outputs[3])) {
            return 0;
        }
    }
    if (!constant_scalar_f32(session, match->inputs[0][1], 1.4142135381698608f) ||
        !constant_scalar_f32(session,
                             match->inputs[2][0] == match->outputs[1] ? match->inputs[2][1]
                                                                        : match->inputs[2][0],
                             1.0f) ||
        !constant_scalar_f32(session,
                             match->inputs[4][0] == match->outputs[3] ? match->inputs[4][1]
                                                                        : match->inputs[4][0],
                             0.5f)) {
        return 0;
    }
    source = &session->tensors[match->inputs[0][0]];
    output = &session->tensors[match->outputs[4]];
    if (source->dtype != LW_DTYPE_F32 || output->dtype != LW_DTYPE_F32 ||
        source->byte_size == 0u || source->byte_size != output->byte_size) {
        return 0;
    }
    for (index = 0u; index < 4u; ++index) {
        const lw_runtime_tensor* temporary = &session->tensors[match->outputs[index]];
        if (temporary->dtype != LW_DTYPE_F32 || temporary->byte_size != source->byte_size) {
            return 0;
        }
    }
    return 1;
}

#if defined(LW_EXPERIMENTAL_FUSION_MEMORY)
static int32_t tensor_last_use_node(const lw_session* session, uint32_t tensor_index) {
    const lw_model* model = session->model;
    int32_t last_use = -1;
    uint32_t node_index;
    for (node_index = 0u; node_index < model->info.node_count; ++node_index) {
        const uint8_t* node = model->bytes + (size_t)model->node_offset +
                              (size_t)node_index * LWM_V0_NODE_SIZE;
        uint16_t input_count = lwm_read_u16(node + 2u);
        uint32_t input_index;
        for (input_index = 0u; input_index < input_count; ++input_index) {
            if (lwm_read_u32(node + 8u + (size_t)input_index * sizeof(uint32_t)) == tensor_index) {
                last_use = (int32_t)node_index;
            }
        }
    }
    for (node_index = 0u; node_index < model->info.output_count; ++node_index) {
        if (lwm_read_u32(model->bytes + (size_t)model->output_offset +
                         (size_t)node_index * sizeof(uint32_t)) == tensor_index) {
            last_use = (int32_t)model->info.node_count;
        }
    }
    return last_use;
}
#endif

#if defined(LW_EXPERIMENTAL_CTC_TILED)
static int match_ctc_greedy_tail(const lw_session* session, uint32_t* skip_tensor) {
    const lw_model* model;
    const uint8_t* node;
    const uint8_t* params;
    const lw_runtime_tensor* logits;
    const lw_runtime_tensor* output;
    uint32_t graph_output;
    uint32_t input_index;
    uint32_t output_index;
    int32_t axis;
    uint32_t index;
    if (session == NULL || skip_tensor == NULL || session->model == NULL ||
        session->ctc_greedy_plan_enabled == 0u) {
        return 0;
    }
    model = session->model;
    if (model->info.input_count != 1u || model->info.output_count != 1u ||
        model->info.node_count == 0u) {
        return 0;
    }
    node = model->bytes + (size_t)model->node_offset +
           (size_t)(model->info.node_count - 1u) * LWM_V0_NODE_SIZE;
    if (lwm_read_u16(node) != LW_PLAN_OP_SOFTMAX || lwm_read_u16(node + 2u) != 1u ||
        lwm_read_u16(node + 4u) != 1u) {
        return 0;
    }
    input_index = lwm_read_u32(node + 8u);
    output_index = lwm_read_u32(node + 40u);
    graph_output = lwm_read_u32(model->bytes + (size_t)model->output_offset);
    if (output_index != graph_output || input_index >= model->info.tensor_count ||
        output_index >= model->info.tensor_count) {
        return 0;
    }
    logits = &session->tensors[input_index];
    output = &session->tensors[output_index];
    if (logits->dtype != LW_DTYPE_F32 || output->dtype != LW_DTYPE_F32 || logits->rank < 2u ||
        logits->rank != output->rank || output->byte_size == 0u) {
        return 0;
    }
    for (index = 0u; index < logits->rank; ++index) {
        if (logits->dimensions[index] != output->dimensions[index]) {
            return 0;
        }
    }
    params = model->bytes + (size_t)lwm_read_u64(node + 56u);
    axis = lwm_read_i32(params + 4u);
    if (axis < 0) {
        axis += (int32_t)logits->rank;
    }
    if (axis != (int32_t)logits->rank - 1) {
        return 0;
    }
    *skip_tensor = output_index;
    return 1;
}

static lw_status prepare_ctc_logits_scratch(lw_session* session, lw_error* error) {
    const lw_runtime_tensor* output;
    uint32_t graph_output;
    uint32_t class_count;
    uint32_t time_steps;
    uint32_t rows;
    uint64_t elements;
    if (session->ctc_greedy_skip_tensor == UINT32_MAX ||
        !lw_simd_level_is_avx2(session->cpu.simd)) {
        return LW_STATUS_OK;
    }
    graph_output = session->ctc_greedy_skip_tensor;
    output = &session->tensors[graph_output];
    /* The tiled kernel currently targets the packed REC shape [1,T,V].
     * Other terminal Softmax shapes retain the safe output-elision plan but
     * do not reserve a scratch buffer that the executor cannot consume. */
    if (output->rank != 3u || output->dimensions[0] != 1 ||
        output->dimensions[output->rank - 1u] <= 0 ||
        output->dimensions[output->rank - 2u] <= 0) {
        return LW_STATUS_OK;
    }
    class_count = (uint32_t)output->dimensions[output->rank - 1u];
    time_steps = (uint32_t)output->dimensions[output->rank - 2u];
    rows = time_steps < 8u ? time_steps : 8u;
    elements = (uint64_t)rows * class_count;
    if (rows == 0u || elements > (uint64_t)(SIZE_MAX / sizeof(float))) {
        lw_set_error(error, LW_STATUS_OUT_OF_BOUNDS, "CTC greedy scratch size overflows");
        return LW_STATUS_OUT_OF_BOUNDS;
    }
    session->ctc_logits_scratch = (float*)malloc((size_t)elements * sizeof(float));
    if (session->ctc_logits_scratch == NULL) {
        lw_set_error(error, LW_STATUS_OUT_OF_MEMORY, "unable to allocate CTC greedy scratch");
        return LW_STATUS_OUT_OF_MEMORY;
    }
    session->ctc_logits_scratch_rows = rows;
    session->ctc_logits_scratch_elements = elements;
    return LW_STATUS_OK;
}
#endif

void lw_free_execution_plan(lw_session* session) {
    if (session == NULL) {
        return;
    }
    free(session->fused_gelu_start_nodes);
    free(session->fused_gelu_skip_tensors);
    free(session->ctc_logits_scratch);
    session->fused_gelu_start_nodes = NULL;
    session->fused_gelu_skip_tensors = NULL;
    session->ctc_greedy_skip_tensor = UINT32_MAX;
    session->ctc_logits_scratch = NULL;
    session->ctc_logits_scratch_rows = 0u;
    session->ctc_logits_scratch_elements = 0u;
}

lw_status lw_prepare_execution_plan(lw_session* session, lw_error* error) {
    uint32_t node_index;
    lw_free_execution_plan(session);
#if defined(LW_EXPERIMENTAL_CTC_TILED)
    (void)match_ctc_greedy_tail(session, &session->ctc_greedy_skip_tensor);
    {
        lw_status scratch_status = prepare_ctc_logits_scratch(session, error);
        if (scratch_status != LW_STATUS_OK) {
            lw_free_execution_plan(session);
            return scratch_status;
        }
    }
#endif
#if defined(LW_EXPERIMENTAL_FUSION_MEMORY)
    if (!lw_simd_level_is_avx2(session->cpu.simd)) {
        lw_set_error(error, LW_STATUS_OK, "");
        return LW_STATUS_OK;
    }
    if (session->model->info.node_count != 0u) {
        session->fused_gelu_start_nodes = (uint8_t*)calloc(
            session->model->info.node_count, sizeof(*session->fused_gelu_start_nodes));
    }
    if (session->model->info.tensor_count != 0u) {
        session->fused_gelu_skip_tensors = (uint8_t*)calloc(
            session->model->info.tensor_count, sizeof(*session->fused_gelu_skip_tensors));
    }
    if ((session->model->info.node_count != 0u && session->fused_gelu_start_nodes == NULL) ||
        (session->model->info.tensor_count != 0u && session->fused_gelu_skip_tensors == NULL)) {
        lw_free_execution_plan(session);
        lw_set_error(error, LW_STATUS_OUT_OF_MEMORY, "unable to allocate fused execution plan");
        return LW_STATUS_OUT_OF_MEMORY;
    }
    for (node_index = 0u; node_index < session->model->info.node_count; ++node_index) {
        lw_fused_gelu_match match;
        uint32_t output_index;
        if (!lw_match_fused_gelu(session, node_index, &match)) {
            continue;
        }
        for (output_index = 0u; output_index < 4u; ++output_index) {
            if (tensor_last_use_node(session, match.outputs[output_index]) !=
                (int32_t)(node_index + output_index + 1u)) {
                break;
            }
        }
        if (output_index != 4u) {
            continue;
        }
        session->fused_gelu_start_nodes[node_index] = 1u;
        session->fused_gelu_skip_tensors[match.outputs[0]] = 1u;
        session->fused_gelu_skip_tensors[match.outputs[1]] = 1u;
        session->fused_gelu_skip_tensors[match.outputs[2]] = 1u;
        session->fused_gelu_skip_tensors[match.outputs[3]] = 1u;
        node_index += 4u;
    }
#else
    (void)node_index;
#endif
    lw_set_error(error, LW_STATUS_OK, "");
    return LW_STATUS_OK;
}

int lw_execution_plan_is_fused_gelu_start(const lw_session* session, uint32_t node_index) {
#if defined(LW_EXPERIMENTAL_FUSION_MEMORY)
    return session != NULL && session->fused_gelu_start_nodes != NULL &&
           node_index < session->model->info.node_count &&
           session->fused_gelu_start_nodes[node_index] != 0u;
#else
    (void)session;
    (void)node_index;
    return 0;
#endif
}

int lw_execution_plan_is_fused_gelu_skip(const lw_session* session, uint32_t tensor_index) {
#if defined(LW_EXPERIMENTAL_FUSION_MEMORY)
    return session != NULL && session->fused_gelu_skip_tensors != NULL &&
           tensor_index < session->model->info.tensor_count &&
           session->fused_gelu_skip_tensors[tensor_index] != 0u;
#else
    (void)session;
    (void)tensor_index;
    return 0;
#endif
}

int lw_execution_plan_is_ctc_greedy_skip(const lw_session* session, uint32_t tensor_index) {
#if defined(LW_EXPERIMENTAL_CTC_TILED)
    return session != NULL && session->ctc_greedy_skip_tensor == tensor_index;
#else
    (void)session;
    (void)tensor_index;
    return 0;
#endif
}

int lw_execution_plan_is_skipped_tensor(const lw_session* session, uint32_t tensor_index) {
    return lw_execution_plan_is_fused_gelu_skip(session, tensor_index) ||
           lw_execution_plan_is_ctc_greedy_skip(session, tensor_index);
}
