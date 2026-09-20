#ifndef LW_CROP_BUFFER_INTERNAL_H
#define LW_CROP_BUFFER_INTERNAL_H

#include "lw_infer.h"

#include <stdint.h>

typedef struct lw_crop_buffer {
    uint8_t* data;
    uint64_t capacity;
    uint64_t retained_limit;
} lw_crop_buffer;

void lw_crop_buffer_init(lw_crop_buffer* buffer, uint64_t retained_limit);
void lw_crop_buffer_free(lw_crop_buffer* buffer);
lw_status lw_crop_buffer_reserve(lw_crop_buffer* buffer, uint64_t required_bytes,
                                 lw_error* error);
void lw_crop_buffer_trim_after_request(lw_crop_buffer* buffer);

#endif