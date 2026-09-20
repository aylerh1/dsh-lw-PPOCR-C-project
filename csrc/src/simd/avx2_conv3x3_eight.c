#include "simd_kernels.h"

#if defined(_M_IX86) || defined(_M_X64) || defined(__i386__) || defined(__x86_64__)
#  include <immintrin.h>
#  define LW_COMPILES_AVX2_CONV3X3_EIGHT 1
#else
#  define LW_COMPILES_AVX2_CONV3X3_EIGHT 0
#endif

#if LW_COMPILES_AVX2_CONV3X3_EIGHT && defined(LW_EXPERIMENTAL_AVX2_CONV3X3_EIGHT_OUTPUTS)
#  if defined(__GNUC__) || defined(__clang__)
__attribute__((target("avx2,no-fma")))
#  endif
void lw_avx2_conv3x3_unit_pad1_eight_outputs_f32(
    const float* input, const float* weights, const float* bias, float* output,
    const int32_t input_dimensions[4], const int32_t output_dimensions[4]) {
    const uint32_t input_channels = (uint32_t)input_dimensions[1];
    const uint32_t height = (uint32_t)input_dimensions[2];
    const uint32_t width = (uint32_t)input_dimensions[3];
    const uint32_t output_channels = (uint32_t)output_dimensions[1];
    const uint64_t channel_plane = (uint64_t)height * width;
    const uint64_t weights_per_output = (uint64_t)input_channels * 9u;
    uint32_t batch;

    for (batch = 0u; batch < (uint32_t)input_dimensions[0]; ++batch) {
        const float* batch_input =
            input + (size_t)((uint64_t)batch * input_channels * channel_plane);
        float* batch_output =
            output + (size_t)((uint64_t)batch * output_channels * channel_plane);
        uint32_t output_channel;
        for (output_channel = 0u; output_channel < output_channels; output_channel += 8u) {
            const float* output_weights[8];
            float* output_planes[8];
            float initial[8];
            uint32_t lane;
            for (lane = 0u; lane < 8u; ++lane) {
                output_weights[lane] =
                    weights + (size_t)((uint64_t)(output_channel + lane) * weights_per_output);
                output_planes[lane] =
                    batch_output + (size_t)((uint64_t)(output_channel + lane) * channel_plane);
                initial[lane] = bias == NULL ? 0.0f : bias[output_channel + lane];
            }

            uint32_t output_y;
            for (output_y = 0u; output_y < height; ++output_y) {
                const size_t output_row_offset = (size_t)((uint64_t)output_y * width);
                uint32_t output_x = 0u;
                while (output_x < width) {
                    const int full_height = output_y != 0u && output_y + 1u < height;
                    const int full_width = output_x != 0u && output_x + 8u < width;
                    if (full_height && full_width) {
                        __m256 accumulators[8];
                        uint32_t input_channel;
                        for (lane = 0u; lane < 8u; ++lane) {
                            accumulators[lane] = _mm256_set1_ps(initial[lane]);
                        }
                        for (input_channel = 0u; input_channel < input_channels;
                             ++input_channel) {
                            const float* input_channel_data =
                                batch_input + (size_t)((uint64_t)input_channel * channel_plane);
                            const size_t weight_offset = (size_t)((uint64_t)input_channel * 9u);
                            uint32_t kernel_y;
                            for (kernel_y = 0u; kernel_y < 3u; ++kernel_y) {
                                const uint32_t input_y = output_y + kernel_y - 1u;
                                const float* input_row =
                                    input_channel_data + (size_t)((uint64_t)input_y * width);
                                uint32_t kernel_x;
                                for (kernel_x = 0u; kernel_x < 3u; ++kernel_x) {
                                    const uint32_t input_x = output_x + kernel_x - 1u;
                                    const size_t weight_index =
                                        weight_offset + (size_t)kernel_y * 3u + kernel_x;
                                    const __m256 input_values =
                                        _mm256_loadu_ps(input_row + input_x);
                                    for (lane = 0u; lane < 8u; ++lane) {
                                        accumulators[lane] = _mm256_add_ps(
                                            accumulators[lane],
                                            _mm256_mul_ps(
                                                input_values,
                                                _mm256_set1_ps(output_weights[lane][weight_index])));
                                    }
                                }
                            }
                        }
                        for (lane = 0u; lane < 8u; ++lane) {
                            _mm256_storeu_ps(
                                output_planes[lane] + output_row_offset + output_x,
                                accumulators[lane]);
                        }
                        output_x += 8u;
                    } else {
                        float accumulators[8];
                        uint32_t input_channel;
                        for (lane = 0u; lane < 8u; ++lane) {
                            accumulators[lane] = initial[lane];
                        }
                        for (input_channel = 0u; input_channel < input_channels;
                             ++input_channel) {
                            const float* input_channel_data =
                                batch_input + (size_t)((uint64_t)input_channel * channel_plane);
                            const size_t weight_offset = (size_t)((uint64_t)input_channel * 9u);
                            uint32_t kernel_y;
                            for (kernel_y = 0u; kernel_y < 3u; ++kernel_y) {
                                const int64_t input_y = (int64_t)output_y + kernel_y - 1;
                                uint32_t kernel_x;
                                if (input_y < 0 || input_y >= (int64_t)height) {
                                    continue;
                                }
                                for (kernel_x = 0u; kernel_x < 3u; ++kernel_x) {
                                    const int64_t input_x = (int64_t)output_x + kernel_x - 1;
                                    if (input_x >= 0 && input_x < (int64_t)width) {
                                        const size_t input_index =
                                            (size_t)((uint64_t)(uint32_t)input_y * width +
                                                     (uint32_t)input_x);
                                        const size_t weight_index =
                                            weight_offset + (size_t)kernel_y * 3u + kernel_x;
                                        const float value = input_channel_data[input_index];
                                        for (lane = 0u; lane < 8u; ++lane) {
                                            accumulators[lane] +=
                                                value * output_weights[lane][weight_index];
                                        }
                                    }
                                }
                            }
                        }
                        for (lane = 0u; lane < 8u; ++lane) {
                            output_planes[lane][output_row_offset + output_x] =
                                accumulators[lane];
                        }
                        ++output_x;
                    }
                }
            }
        }
    }
}
#endif
