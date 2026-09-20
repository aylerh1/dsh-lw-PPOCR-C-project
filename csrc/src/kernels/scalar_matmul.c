#include "scalar_kernels.h"

/* Shared-weight matrix multiplication used by the converted OCR graphs. */
#include "cpu_features.h"
#include "simd_kernels.h"

#include <stddef.h>
#include <stdint.h>

static int multiply_fits(uint64_t left, uint64_t right, uint64_t* result) {
    if (right != 0u && left > UINT64_MAX / right) {
        return 0;
    }
    *result = left * right;
    return 1;
}

static int buffer_fits(uint32_t first, uint32_t second, uint32_t third) {
    uint64_t count;
    if (!multiply_fits(first, second, &count) || !multiply_fits(count, third, &count)) {
        return 0;
    }
    return count <= (uint64_t)(SIZE_MAX / sizeof(float));
}

static lw_status validate_matmul_shared_f32(const float* input, const float* weights, float* output,
                                            uint32_t batch_count, uint32_t rows,
                                            uint32_t inner_dimension, uint32_t columns) {
    if (input == NULL || weights == NULL || output == NULL || output == input ||
        output == weights) {
        return LW_STATUS_INVALID_ARGUMENT;
    }
    if (batch_count == 0u || rows == 0u || inner_dimension == 0u || columns == 0u) {
        return LW_STATUS_INVALID_SHAPE;
    }
    if (!buffer_fits(batch_count, rows, inner_dimension) ||
        !buffer_fits(1u, inner_dimension, columns) || !buffer_fits(batch_count, rows, columns)) {
        return LW_STATUS_OUT_OF_BOUNDS;
    }
    return LW_STATUS_OK;
}

static void scalar_matmul_shared_f32(const float* input, const float* weights, float* output,
                                     uint32_t batch_count, uint32_t rows, uint32_t inner_dimension,
                                     uint32_t columns) {
    uint32_t batch;
    uint32_t row;
    uint32_t column;
    /* Four output rows share each sequential scan of the weight matrix. This
     * improves cache locality while remaining the scalar correctness path. */
    for (batch = 0u; batch < batch_count; ++batch) {
        for (row = 0u; row < rows;) {
            uint32_t row_end = rows - row < 4u ? rows : row + 4u;
            uint32_t inner;
            uint32_t current_row;
            for (current_row = row; current_row < row_end; ++current_row) {
                uint64_t output_base = ((uint64_t)batch * rows + current_row) * columns;
                for (column = 0u; column < columns; ++column) {
                    output[(size_t)(output_base + column)] = 0.0f;
                }
            }
            for (inner = 0u; inner < inner_dimension; ++inner) {
                uint64_t weight_base = (uint64_t)inner * columns;
                for (current_row = row; current_row < row_end; ++current_row) {
                    uint64_t input_base = ((uint64_t)batch * rows + current_row) * inner_dimension;
                    uint64_t output_base = ((uint64_t)batch * rows + current_row) * columns;
                    float input_value = input[(size_t)(input_base + inner)];
                    for (column = 0u; column < columns; ++column) {
                        output[(size_t)(output_base + column)] +=
                            input_value * weights[(size_t)(weight_base + column)];
                    }
                }
            }
            row = row_end;
        }
    }
}

static int matmul_shape_product(const int32_t* dimensions, uint32_t rank, uint64_t* result) {
    uint32_t index;
    uint64_t product = 1u;
    if (dimensions == NULL || rank == 0u || rank > LW_MAX_DIMS) {
        return 0;
    }
    for (index = 0u; index < rank; ++index) {
        if (dimensions[index] <= 0 || product > UINT64_MAX / (uint32_t)dimensions[index]) {
            return 0;
        }
        product *= (uint32_t)dimensions[index];
    }
    *result = product;
    return product <= (uint64_t)(SIZE_MAX / sizeof(float));
}

/* Portable ONNX MatMul for rank >= 2 tensors with broadcast batch dimensions.
 * The common rank-N x rank-2 constant-weight case stays on the optimized
 * shared-weight path in the executor; this fallback covers attention-style
 * batched matrices used by larger REC graphs. */
lw_status lw_scalar_matmul_f32(const float* input, const float* weights, float* output,
                               uint32_t input_rank, const int32_t* input_dimensions,
                               uint32_t weights_rank, const int32_t* weights_dimensions,
                               uint32_t output_rank, const int32_t* output_dimensions) {
    uint32_t batch_rank;
    uint32_t input_batch_rank;
    uint32_t weights_batch_rank;
    uint32_t input_offset;
    uint32_t weights_offset;
    uint32_t batch_index;
    uint32_t batch_count;
    uint32_t row;
    uint32_t column;
    uint32_t inner;
    uint64_t input_elements;
    uint64_t weights_elements;
    uint64_t output_elements;
    uint64_t input_matrix_size;
    uint64_t weights_matrix_size;
    uint64_t output_matrix_size;
    if (input == NULL || weights == NULL || output == NULL || output == input ||
        output == weights || input_dimensions == NULL || weights_dimensions == NULL ||
        output_dimensions == NULL || input_rank < 2u || weights_rank < 2u ||
        input_rank > LW_MAX_DIMS || weights_rank > LW_MAX_DIMS || output_rank < 2u ||
        output_rank > LW_MAX_DIMS) {
        return LW_STATUS_INVALID_ARGUMENT;
    }
    input_batch_rank = input_rank - 2u;
    weights_batch_rank = weights_rank - 2u;
    batch_rank = input_batch_rank > weights_batch_rank ? input_batch_rank : weights_batch_rank;
    if (batch_rank > LW_MAX_DIMS - 2u || output_rank != batch_rank + 2u ||
        input_dimensions[input_rank - 1u] != weights_dimensions[weights_rank - 2u] ||
        input_dimensions[input_rank - 2u] != output_dimensions[batch_rank] ||
        weights_dimensions[weights_rank - 1u] != output_dimensions[batch_rank + 1u]) {
        return LW_STATUS_INVALID_SHAPE;
    }
    input_offset = batch_rank - input_batch_rank;
    weights_offset = batch_rank - weights_batch_rank;
    batch_count = 1u;
    for (batch_index = 0u; batch_index < batch_rank; ++batch_index) {
        int32_t input_dimension = batch_index < input_offset
                                      ? 1
                                      : input_dimensions[batch_index - input_offset];
        int32_t weights_dimension = batch_index < weights_offset
                                        ? 1
                                        : weights_dimensions[batch_index - weights_offset];
        if (input_dimension <= 0 || weights_dimension <= 0 ||
            (input_dimension != weights_dimension && input_dimension != 1 &&
             weights_dimension != 1) || output_dimensions[batch_index] <= 0 ||
            (output_dimensions[batch_index] != input_dimension &&
             output_dimensions[batch_index] != weights_dimension)) {
            return LW_STATUS_INVALID_SHAPE;
        }
        if (batch_count > UINT32_MAX / (uint32_t)output_dimensions[batch_index]) {
            return LW_STATUS_OUT_OF_BOUNDS;
        }
        batch_count *= (uint32_t)output_dimensions[batch_index];
    }
    if (!matmul_shape_product(input_dimensions, input_rank, &input_elements) ||
        !matmul_shape_product(weights_dimensions, weights_rank, &weights_elements) ||
        !matmul_shape_product(output_dimensions, output_rank, &output_elements)) {
        return LW_STATUS_OUT_OF_BOUNDS;
    }
    (void)input_elements;
    (void)weights_elements;
    (void)output_elements;
    input_matrix_size = (uint64_t)(uint32_t)input_dimensions[input_rank - 2u] *
                        (uint32_t)input_dimensions[input_rank - 1u];
    weights_matrix_size = (uint64_t)(uint32_t)weights_dimensions[weights_rank - 2u] *
                          (uint32_t)weights_dimensions[weights_rank - 1u];
    output_matrix_size = (uint64_t)(uint32_t)output_dimensions[batch_rank] *
                         (uint32_t)output_dimensions[batch_rank + 1u];
    for (batch_index = 0u; batch_index < batch_count; ++batch_index) {
        uint32_t remaining = batch_index;
        uint64_t input_batch = 0u;
        uint64_t weights_batch = 0u;
        uint32_t axis;
        for (axis = batch_rank; axis-- > 0u;) {
            uint32_t coordinate = remaining % (uint32_t)output_dimensions[axis];
            int32_t input_dimension = axis < input_offset
                                          ? 1
                                          : input_dimensions[axis - input_offset];
            int32_t weights_dimension = axis < weights_offset
                                            ? 1
                                            : weights_dimensions[axis - weights_offset];
            remaining /= (uint32_t)output_dimensions[axis];
            if (axis >= input_offset) {
                input_batch = input_batch * (uint32_t)input_dimension +
                              (input_dimension == 1 ? 0u : coordinate);
            }
            if (axis >= weights_offset) {
                weights_batch = weights_batch * (uint32_t)weights_dimension +
                                (weights_dimension == 1 ? 0u : coordinate);
            }
        }
        for (row = 0u; row < (uint32_t)input_dimensions[input_rank - 2u]; ++row) {
            for (column = 0u; column < (uint32_t)weights_dimensions[weights_rank - 1u];
                 ++column) {
                float value = 0.0f;
                for (inner = 0u; inner < (uint32_t)input_dimensions[input_rank - 1u]; ++inner) {
                    size_t input_index = (size_t)(input_batch * input_matrix_size +
                                                  (uint64_t)row *
                                                      (uint32_t)input_dimensions[input_rank - 1u] +
                                                  inner);
                    size_t weights_index = (size_t)(weights_batch * weights_matrix_size +
                                                    (uint64_t)inner *
                                                        (uint32_t)weights_dimensions[weights_rank - 1u] +
                                                    column);
                    value += input[input_index] * weights[weights_index];
                }
                output[(size_t)(batch_index * output_matrix_size +
                                (uint64_t)row *
                                    (uint32_t)weights_dimensions[weights_rank - 1u] +
                                column)] = value;
            }
        }
    }
    return LW_STATUS_OK;
}

lw_status lw_scalar_matmul_shared_f32(const float* input, const float* weights, float* output,
                                      uint32_t batch_count, uint32_t rows, uint32_t inner_dimension,
                                      uint32_t columns) {
    lw_status status = validate_matmul_shared_f32(input, weights, output, batch_count, rows,
                                                  inner_dimension, columns);
    if (status != LW_STATUS_OK) {
        return status;
    }
    scalar_matmul_shared_f32(input, weights, output, batch_count, rows, inner_dimension, columns);
    return LW_STATUS_OK;
}

lw_status lw_matmul_shared_f32(const float* input, const float* weights, float* output,
                               uint32_t batch_count, uint32_t rows, uint32_t inner_dimension,
                               uint32_t columns) {
    lw_simd_level simd_level;
    lw_status status = validate_matmul_shared_f32(input, weights, output, batch_count, rows,
                                                  inner_dimension, columns);
    if (status != LW_STATUS_OK) {
        return status;
    }
    /* Detection happens at runtime so one binary remains safe on older CPUs. */
    simd_level = lw_detect_simd_level();
    if (lw_simd_level_is_avx2(simd_level)) {
        lw_avx2_matmul_shared_f32(input, weights, output, batch_count, rows, inner_dimension,
                                  columns);
    } else if (lw_simd_level_is_sse2(simd_level)) {
        lw_sse2_matmul_shared_f32(input, weights, output, batch_count, rows, inner_dimension,
                                  columns);
    } else {
        scalar_matmul_shared_f32(input, weights, output, batch_count, rows, inner_dimension,
                                 columns);
    }
    return LW_STATUS_OK;
}
