#include "scalar_kernels.h"
#include "cpu_features.h"
#include "simd_kernels.h"

/* Numerically stable softmax: subtract the row maximum before exponentiation. */

#include <math.h>
#include <stddef.h>
#include <stdint.h>

lw_status lw_scalar_softmax_f32(const float* input, float* output, uint32_t rank,
                                const int32_t* dimensions, int32_t axis) {
    uint64_t outer_count = 1u;
    uint64_t inner_count = 1u;
    uint64_t axis_count;
    uint64_t outer;
    uint64_t inner;
    uint32_t normalized_axis;
    uint32_t index;

    if (input == NULL || output == NULL) {
        return LW_STATUS_INVALID_ARGUMENT;
    }
    if (rank == 0u || rank > LW_MAX_DIMS || dimensions == NULL || axis < -(int32_t)rank ||
        axis >= (int32_t)rank) {
        return LW_STATUS_INVALID_SHAPE;
    }
    normalized_axis = axis < 0 ? (uint32_t)((int32_t)rank + axis) : (uint32_t)axis;
    for (index = 0u; index < rank; ++index) {
        uint64_t dimension;
        if (dimensions[index] <= 0) {
            return LW_STATUS_INVALID_SHAPE;
        }
        dimension = (uint32_t)dimensions[index];
        if (index < normalized_axis) {
            if (outer_count > UINT64_MAX / dimension) {
                return LW_STATUS_OUT_OF_BOUNDS;
            }
            outer_count *= dimension;
        } else if (index > normalized_axis) {
            if (inner_count > UINT64_MAX / dimension) {
                return LW_STATUS_OUT_OF_BOUNDS;
            }
            inner_count *= dimension;
        }
    }
    axis_count = (uint32_t)dimensions[normalized_axis];
    if (outer_count > UINT64_MAX / axis_count ||
        outer_count * axis_count > UINT64_MAX / inner_count ||
        outer_count * axis_count * inner_count > (uint64_t)(SIZE_MAX / sizeof(float))) {
        return LW_STATUS_OUT_OF_BOUNDS;
    }

    /* The REC classifier axis is the contiguous final dimension. Direct row
     * pointers remove the general strided offset arithmetic while preserving
     * the maximum, expf, accumulation and division order exactly. */
    if (inner_count == 1u) {
        if (axis_count >= 256u &&
            lw_simd_level_is_avx2(lw_detect_simd_level())) {
            lw_avx2_softmax_contiguous_f32(input, output, outer_count, axis_count);
            return LW_STATUS_OK;
        }
        for (outer = 0u; outer < outer_count; ++outer) {
            const float* input_row = input + (size_t)(outer * axis_count);
            float* output_row = output + (size_t)(outer * axis_count);
            float maximum = input_row[0];
            float sum = 0.0f;
            uint64_t axis_index;
            for (axis_index = 1u; axis_index < axis_count; ++axis_index) {
                if (input_row[(size_t)axis_index] > maximum) {
                    maximum = input_row[(size_t)axis_index];
                }
            }
            for (axis_index = 0u; axis_index < axis_count; ++axis_index) {
                float value = expf(input_row[(size_t)axis_index] - maximum);
                output_row[(size_t)axis_index] = value;
                sum += value;
            }
            for (axis_index = 0u; axis_index < axis_count; ++axis_index) {
                output_row[(size_t)axis_index] /= sum;
            }
        }
        return LW_STATUS_OK;
    }

    for (outer = 0u; outer < outer_count; ++outer) {
        for (inner = 0u; inner < inner_count; ++inner) {
            uint64_t axis_index;
            uint64_t base = outer * axis_count * inner_count + inner;
            /* Shifting by the maximum preserves the result and prevents expf
             * from overflowing on large logits. */
            float maximum = input[(size_t)base];
            float sum = 0.0f;
            for (axis_index = 1u; axis_index < axis_count; ++axis_index) {
                float value = input[(size_t)(base + axis_index * inner_count)];
                if (value > maximum) {
                    maximum = value;
                }
            }
            for (axis_index = 0u; axis_index < axis_count; ++axis_index) {
                size_t offset = (size_t)(base + axis_index * inner_count);
                float value = expf(input[offset] - maximum);
                output[offset] = value;
                sum += value;
            }
            for (axis_index = 0u; axis_index < axis_count; ++axis_index) {
                size_t offset = (size_t)(base + axis_index * inner_count);
                output[offset] /= sum;
            }
        }
    }
    return LW_STATUS_OK;
}

lw_status lw_ctc_greedy_softmax_contiguous_f32(const float* input, uint32_t* best_indices,
                                               float* emitted_probabilities,
                                               uint64_t row_count, uint64_t axis_count) {
    uint64_t row;
    if (input == NULL || best_indices == NULL || emitted_probabilities == NULL || row_count == 0u ||
        axis_count == 0u || axis_count > UINT32_MAX ||
        row_count > (uint64_t)(SIZE_MAX / sizeof(*best_indices)) ||
        row_count > (uint64_t)(SIZE_MAX / sizeof(*emitted_probabilities)) ||
        row_count > UINT64_MAX / axis_count ||
        row_count * axis_count > (uint64_t)(SIZE_MAX / sizeof(float))) {
        return LW_STATUS_INVALID_ARGUMENT;
    }
    /* Argmax is required for every time step, but the CTC confidence score
     * consumes Softmax only for non-blank, non-repeated emitted classes. */
    for (row = 0u; row < row_count; ++row) {
        const float* input_row = input + (size_t)(row * axis_count);
        uint32_t best_index = 0u;
        float maximum = input_row[0];
        uint64_t index;
        if (!isfinite(maximum)) {
            return LW_STATUS_INVALID_ARGUMENT;
        }
        for (index = 1u; index < axis_count; ++index) {
            float value = input_row[(size_t)index];
            if (!isfinite(value)) {
                return LW_STATUS_INVALID_ARGUMENT;
            }
            if (value > maximum) {
                maximum = value;
                best_index = (uint32_t)index;
            }
        }
        best_indices[(size_t)row] = best_index;
    }
    return lw_ctc_emitted_softmax_contiguous_f32(input, best_indices,
                                                 emitted_probabilities, row_count, axis_count);
}

lw_status lw_ctc_emitted_softmax_contiguous_f32(const float* input,
                                                const uint32_t* best_indices,
                                                float* emitted_probabilities,
                                                uint64_t row_count, uint64_t axis_count) {
    uint64_t row;
    if (input == NULL || best_indices == NULL || emitted_probabilities == NULL ||
        row_count == 0u || axis_count == 0u || axis_count > UINT32_MAX ||
        row_count > (uint64_t)(SIZE_MAX / sizeof(*best_indices)) ||
        row_count > (uint64_t)(SIZE_MAX / sizeof(*emitted_probabilities)) ||
        row_count > UINT64_MAX / axis_count ||
        row_count * axis_count > (uint64_t)(SIZE_MAX / sizeof(float))) {
        return LW_STATUS_INVALID_ARGUMENT;
    }
    for (row = 0u; row < row_count; ++row) {
        if (best_indices[(size_t)row] >= axis_count) {
            return LW_STATUS_INVALID_ARGUMENT;
        }
        emitted_probabilities[(size_t)row] = 0.0f;
    }
    if (axis_count >= 256u && lw_simd_level_is_avx2(lw_detect_simd_level())) {
        lw_avx2_ctc_emitted_softmax_contiguous_f32(input, best_indices,
                                                   emitted_probabilities, row_count, axis_count);
        return LW_STATUS_OK;
    }
    for (row = 0u; row < row_count; ++row) {
        uint32_t best_index = best_indices[(size_t)row];
        if (best_index != 0u && (row == 0u || best_index != best_indices[(size_t)row - 1u])) {
            const float* input_row = input + (size_t)(row * axis_count);
            float maximum = input_row[best_index];
            float sum = 0.0f;
            uint64_t index;
            for (index = 0u; index < axis_count; ++index) {
                sum += expf(input_row[(size_t)index] - maximum);
            }
            emitted_probabilities[(size_t)row] = 1.0f / sum;
        }
    }
    return LW_STATUS_OK;
}
