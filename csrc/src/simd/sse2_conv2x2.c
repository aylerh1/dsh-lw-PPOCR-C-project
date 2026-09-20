#include "simd_kernels.h"
#include "simd_platform.h"

#include "scalar_kernels.h"

#include <stddef.h>

#define LW_COMPILES_SSE2_CONV2X2 LW_SIMD_HAS_SSE2_INTRINSICS

#if LW_COMPILES_SSE2_CONV2X2
LW_SIMD_SSE2_TARGET
static void conv2x2_unit_pad_end1_four_outputs_f32(
    const float* input, const float* weights, const float* bias, float* output,
    const int32_t input_dimensions[4], const int32_t output_dimensions[4]) {
    uint32_t height = (uint32_t)input_dimensions[2];
    uint32_t width = (uint32_t)input_dimensions[3];
    uint64_t channel_plane = (uint64_t)height * width;
    uint32_t batch;
    for (batch = 0u; batch < (uint32_t)input_dimensions[0]; ++batch) {
        const float* batch_input =
            input + (size_t)((uint64_t)batch * (uint32_t)input_dimensions[1] * channel_plane);
        float* batch_output =
            output + (size_t)((uint64_t)batch * (uint32_t)output_dimensions[1] * channel_plane);
        uint32_t output_channel;
        for (output_channel = 0u; output_channel < (uint32_t)output_dimensions[1];
             output_channel += 4u) {
            const uint64_t weights_per_output = (uint64_t)(uint32_t)input_dimensions[1] * 4u;
            const float* weights0 = weights + (size_t)((uint64_t)(output_channel + 0u) * weights_per_output);
            const float* weights1 = weights + (size_t)((uint64_t)(output_channel + 1u) * weights_per_output);
            const float* weights2 = weights + (size_t)((uint64_t)(output_channel + 2u) * weights_per_output);
            const float* weights3 = weights + (size_t)((uint64_t)(output_channel + 3u) * weights_per_output);
            float* output0 = batch_output + (size_t)((uint64_t)(output_channel + 0u) * channel_plane);
            float* output1 = batch_output + (size_t)((uint64_t)(output_channel + 1u) * channel_plane);
            float* output2 = batch_output + (size_t)((uint64_t)(output_channel + 2u) * channel_plane);
            float* output3 = batch_output + (size_t)((uint64_t)(output_channel + 3u) * channel_plane);
            const float initial0 = bias == NULL ? 0.0f : bias[output_channel + 0u];
            const float initial1 = bias == NULL ? 0.0f : bias[output_channel + 1u];
            const float initial2 = bias == NULL ? 0.0f : bias[output_channel + 2u];
            const float initial3 = bias == NULL ? 0.0f : bias[output_channel + 3u];
            __m128 initial_values0 = _mm_set1_ps(initial0);
            __m128 initial_values1 = _mm_set1_ps(initial1);
            __m128 initial_values2 = _mm_set1_ps(initial2);
            __m128 initial_values3 = _mm_set1_ps(initial3);
            uint64_t spatial = 0u;
            for (; spatial + 4u <= channel_plane; spatial += 4u) {
                _mm_storeu_ps(output0 + (size_t)spatial, initial_values0);
                _mm_storeu_ps(output1 + (size_t)spatial, initial_values1);
                _mm_storeu_ps(output2 + (size_t)spatial, initial_values2);
                _mm_storeu_ps(output3 + (size_t)spatial, initial_values3);
            }
            for (; spatial < channel_plane; ++spatial) {
                output0[(size_t)spatial] = initial0;
                output1[(size_t)spatial] = initial1;
                output2[(size_t)spatial] = initial2;
                output3[(size_t)spatial] = initial3;
            }
            uint32_t input_channel;
            for (input_channel = 0u; input_channel < (uint32_t)input_dimensions[1];
                 ++input_channel) {
                const float* input_channel_data =
                    batch_input + (size_t)((uint64_t)input_channel * channel_plane);
                const float* weights0_channel = weights0 + (size_t)input_channel * 4u;
                const float* weights1_channel = weights1 + (size_t)input_channel * 4u;
                const float* weights2_channel = weights2 + (size_t)input_channel * 4u;
                const float* weights3_channel = weights3 + (size_t)input_channel * 4u;
                uint32_t kernel_y;
                for (kernel_y = 0u; kernel_y < 2u; ++kernel_y) {
                    uint32_t output_y;
                    uint32_t output_y_end = height - kernel_y;
                    for (output_y = 0u; output_y < output_y_end; ++output_y) {
                        const float* input_row = input_channel_data +
                            (size_t)((uint64_t)(output_y + kernel_y) * width);
                        float* output_row0 = output0 + (size_t)((uint64_t)output_y * width);
                        float* output_row1 = output1 + (size_t)((uint64_t)output_y * width);
                        float* output_row2 = output2 + (size_t)((uint64_t)output_y * width);
                        float* output_row3 = output3 + (size_t)((uint64_t)output_y * width);
                        uint32_t kernel_x;
                        for (kernel_x = 0u; kernel_x < 2u; ++kernel_x) {
                            uint32_t output_x = 0u;
                            uint32_t output_x_end = width - kernel_x;
                            __m128 weight0 = _mm_set1_ps(weights0_channel[kernel_y * 2u + kernel_x]);
                            __m128 weight1 = _mm_set1_ps(weights1_channel[kernel_y * 2u + kernel_x]);
                            __m128 weight2 = _mm_set1_ps(weights2_channel[kernel_y * 2u + kernel_x]);
                            __m128 weight3 = _mm_set1_ps(weights3_channel[kernel_y * 2u + kernel_x]);
                            const float* input_values = input_row + kernel_x;
                            for (; output_x + 4u <= output_x_end; output_x += 4u) {
                                __m128 values = _mm_loadu_ps(input_values + output_x);
                                __m128 values0 = _mm_loadu_ps(output_row0 + output_x);
                                __m128 values1 = _mm_loadu_ps(output_row1 + output_x);
                                __m128 values2 = _mm_loadu_ps(output_row2 + output_x);
                                __m128 values3 = _mm_loadu_ps(output_row3 + output_x);
                                _mm_storeu_ps(output_row0 + output_x,
                                              _mm_add_ps(values0, _mm_mul_ps(values, weight0)));
                                _mm_storeu_ps(output_row1 + output_x,
                                              _mm_add_ps(values1, _mm_mul_ps(values, weight1)));
                                _mm_storeu_ps(output_row2 + output_x,
                                              _mm_add_ps(values2, _mm_mul_ps(values, weight2)));
                                _mm_storeu_ps(output_row3 + output_x,
                                              _mm_add_ps(values3, _mm_mul_ps(values, weight3)));
                            }
                            for (; output_x < output_x_end; ++output_x) {
                                float value = input_values[output_x];
                                output_row0[output_x] += value * weights0_channel[kernel_y * 2u + kernel_x];
                                output_row1[output_x] += value * weights1_channel[kernel_y * 2u + kernel_x];
                                output_row2[output_x] += value * weights2_channel[kernel_y * 2u + kernel_x];
                                output_row3[output_x] += value * weights3_channel[kernel_y * 2u + kernel_x];
                            }
                        }
                    }
                }
            }
        }
    }
}
#endif

LW_SIMD_SSE2_TARGET
void lw_sse2_conv2x2_unit_pad_end1_f32(const float* input, const float* weights, const float* bias,
                                       float* output, const int32_t input_dimensions[4],
                                       const int32_t output_dimensions[4]) {
#if LW_COMPILES_SSE2_CONV2X2
    if ((uint32_t)output_dimensions[1] % 4u == 0u) {
        conv2x2_unit_pad_end1_four_outputs_f32(input, weights, bias, output, input_dimensions,
                                              output_dimensions);
        return;
    }
    uint32_t height = (uint32_t)input_dimensions[2];
    uint32_t width = (uint32_t)input_dimensions[3];
    uint64_t channel_plane = (uint64_t)height * width;
    uint32_t batch;
    for (batch = 0u; batch < (uint32_t)input_dimensions[0]; ++batch) {
        const float* batch_input =
            input + (size_t)((uint64_t)batch * (uint32_t)input_dimensions[1] * channel_plane);
        float* batch_output =
            output + (size_t)((uint64_t)batch * (uint32_t)output_dimensions[1] * channel_plane);
        uint32_t output_channel;
        for (output_channel = 0u; output_channel < (uint32_t)output_dimensions[1];
             ++output_channel) {
            const float* output_channel_weights =
                weights + (size_t)((uint64_t)output_channel * (uint32_t)input_dimensions[1] * 4u);
            float* output_channel_data =
                batch_output + (size_t)((uint64_t)output_channel * channel_plane);
            float initial = bias == NULL ? 0.0f : bias[output_channel];
            __m128 initial_values = _mm_set1_ps(initial);
            uint64_t spatial = 0u;
            uint32_t input_channel;
            for (; spatial + 4u <= channel_plane; spatial += 4u) {
                _mm_storeu_ps(output_channel_data + (size_t)spatial, initial_values);
            }
            for (; spatial < channel_plane; ++spatial) {
                output_channel_data[(size_t)spatial] = initial;
            }
            for (input_channel = 0u; input_channel < (uint32_t)input_dimensions[1];
                 ++input_channel) {
                const float* input_channel_data =
                    batch_input + (size_t)((uint64_t)input_channel * channel_plane);
                const float* weight_channel_data =
                    output_channel_weights + (size_t)((uint64_t)input_channel * 4u);
                uint32_t kernel_y;
                for (kernel_y = 0u; kernel_y < 2u; ++kernel_y) {
                    uint32_t output_y_end = height - kernel_y;
                    uint32_t kernel_x;
                    for (kernel_x = 0u; kernel_x < 2u; ++kernel_x) {
                        uint32_t output_x_end = width - kernel_x;
                        __m128 weight_values =
                            _mm_set1_ps(weight_channel_data[kernel_y * 2u + kernel_x]);
                        uint32_t output_y;
                        for (output_y = 0u; output_y < output_y_end; ++output_y) {
                            const float* input_row =
                                input_channel_data +
                                (size_t)((uint64_t)(output_y + kernel_y) * width + kernel_x);
                            float* output_row =
                                output_channel_data + (size_t)((uint64_t)output_y * width);
                            uint32_t output_x = 0u;
                            for (; output_x + 4u <= output_x_end; output_x += 4u) {
                                __m128 input_values = _mm_loadu_ps(input_row + output_x);
                                __m128 output_values = _mm_loadu_ps(output_row + output_x);
                                output_values = _mm_add_ps(output_values,
                                                           _mm_mul_ps(input_values, weight_values));
                                _mm_storeu_ps(output_row + output_x, output_values);
                            }
                            for (; output_x < output_x_end; ++output_x) {
                                output_row[output_x] +=
                                    input_row[output_x] *
                                    weight_channel_data[kernel_y * 2u + kernel_x];
                            }
                        }
                    }
                }
            }
        }
    }
#else
    lw_scalar_conv2x2_unit_pad_end1_f32(input, weights, bias, output, input_dimensions,
                                        output_dimensions);
#endif
}
