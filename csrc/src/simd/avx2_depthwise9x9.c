#include "simd_kernels.h"

#include "scalar_kernels.h"

#include <stddef.h>

#if defined(_M_IX86) || defined(_M_X64) || defined(__i386__) || defined(__x86_64__)
#  include <immintrin.h>
#  define LW_COMPILES_AVX2_DEPTHWISE9X9 1
#else
#  define LW_COMPILES_AVX2_DEPTHWISE9X9 0
#endif

#if LW_COMPILES_AVX2_DEPTHWISE9X9 && (defined(__GNUC__) || defined(__clang__))
__attribute__((target("avx2,no-fma")))
#endif
void lw_avx2_depthwise_conv9x9_unit_pad4_f32(const float* input, const float* weights,
                                             const float* bias, float* output,
                                             const int32_t dimensions[4]) {
#if LW_COMPILES_AVX2_DEPTHWISE9X9
    const uint32_t height = (uint32_t)dimensions[2];
    const uint32_t width = (uint32_t)dimensions[3];
    const uint64_t channel_plane = (uint64_t)height * width;
    uint32_t batch;

    for (batch = 0u; batch < (uint32_t)dimensions[0]; ++batch) {
        uint32_t channel;
        for (channel = 0u; channel < (uint32_t)dimensions[1]; ++channel) {
            const uint64_t channel_offset =
                ((uint64_t)batch * (uint32_t)dimensions[1] + channel) * channel_plane;
            const float* input_channel = input + (size_t)channel_offset;
            const float* channel_weights = weights + (size_t)channel * 81u;
            float* output_channel = output + (size_t)channel_offset;
            const float initial = bias == NULL ? 0.0f : bias[channel];
            const __m256 initial_values = _mm256_set1_ps(initial);
            uint64_t spatial = 0u;
            uint32_t kernel_y;

            for (; spatial + 8u <= channel_plane; spatial += 8u) {
                _mm256_storeu_ps(output_channel + (size_t)spatial, initial_values);
            }
            for (; spatial < channel_plane; ++spatial) {
                output_channel[(size_t)spatial] = initial;
            }

            for (kernel_y = 0u; kernel_y < 9u; ++kernel_y) {
                const uint32_t output_y_begin = kernel_y < 4u ? 4u - kernel_y : 0u;
                const uint32_t output_y_trim = kernel_y > 4u ? kernel_y - 4u : 0u;
                const uint32_t output_y_end =
                    output_y_trim < height ? height - output_y_trim : 0u;
                uint32_t kernel_x;
                for (kernel_x = 0u; kernel_x < 9u; ++kernel_x) {
                    const uint32_t output_x_begin = kernel_x < 4u ? 4u - kernel_x : 0u;
                    const uint32_t output_x_trim = kernel_x > 4u ? kernel_x - 4u : 0u;
                    const uint32_t output_x_end =
                        output_x_trim < width ? width - output_x_trim : 0u;
                    const float weight = channel_weights[kernel_y * 9u + kernel_x];
                    const __m256 weight_values = _mm256_set1_ps(weight);
                    uint32_t output_y;
                    for (output_y = output_y_begin; output_y < output_y_end; ++output_y) {
                        const uint32_t input_y = output_y + kernel_y - 4u;
                        const float* input_row =
                            input_channel + (size_t)((uint64_t)input_y * width);
                        float* output_row =
                            output_channel + (size_t)((uint64_t)output_y * width);
                        uint32_t output_x = output_x_begin;
                        for (; output_x + 8u <= output_x_end; output_x += 8u) {
                            const uint32_t input_x = output_x + kernel_x - 4u;
                            const __m256 input_values = _mm256_loadu_ps(input_row + input_x);
                            __m256 output_values = _mm256_loadu_ps(output_row + output_x);
                            output_values = _mm256_add_ps(
                                output_values, _mm256_mul_ps(input_values, weight_values));
                            _mm256_storeu_ps(output_row + output_x, output_values);
                        }
                        for (; output_x < output_x_end; ++output_x) {
                            const uint32_t input_x = output_x + kernel_x - 4u;
                            output_row[output_x] += input_row[input_x] * weight;
                        }
                    }
                }
            }
        }
    }
#else
    const int32_t weight_dimensions[4] = {dimensions[1], 1, 9, 9};
    const int32_t kernel[2] = {9, 9};
    const int32_t strides[2] = {1, 1};
    const int32_t dilations[2] = {1, 1};
    const int32_t pads[4] = {4, 4, 4, 4};
    (void)lw_scalar_conv2d_f32(input, weights, bias, (uint32_t)dimensions[1], output, dimensions,
                               weight_dimensions, dimensions, kernel, strides, dilations, pads,
                               (uint32_t)dimensions[1]);
#endif
}
