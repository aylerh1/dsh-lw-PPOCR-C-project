#include "crop_buffer_internal.h"

#include "error_internal.h"

#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#define LW_CROP_BUFFER_MIN_CAPACITY UINT64_C(4096)

static int crop_size_fits(uint64_t bytes) {
#if SIZE_MAX < UINT64_MAX
    return bytes <= (uint64_t)SIZE_MAX;
#else
    (void)bytes;
    return 1;
#endif
}

static uint64_t next_capacity(uint64_t current, uint64_t required) {
    uint64_t value = current < LW_CROP_BUFFER_MIN_CAPACITY
                          ? LW_CROP_BUFFER_MIN_CAPACITY
                          : current;
    while (value < required) {
        if (value > UINT64_MAX / 2u) {
            return required;
        }
        value *= 2u;
    }
    return value;
}

void lw_crop_buffer_init(lw_crop_buffer* buffer, uint64_t retained_limit) {
    if (buffer == NULL) {
        return;
    }
    memset(buffer, 0, sizeof(*buffer));
    buffer->retained_limit = retained_limit;
}

void lw_crop_buffer_free(lw_crop_buffer* buffer) {
    if (buffer == NULL) {
        return;
    }
    free(buffer->data);
    memset(buffer, 0, sizeof(*buffer));
}

lw_status lw_crop_buffer_reserve(lw_crop_buffer* buffer, uint64_t required_bytes,
                                 lw_error* error) {
    uint64_t capacity;
    uint8_t* resized;

    if (buffer == NULL) {
        lw_set_error(error, LW_STATUS_INVALID_ARGUMENT, "crop buffer is required");
        return LW_STATUS_INVALID_ARGUMENT;
    }
    if (required_bytes <= buffer->capacity) {
        return LW_STATUS_OK;
    }
    if (!crop_size_fits(required_bytes)) {
        lw_set_error(error, LW_STATUS_OUT_OF_BOUNDS,
                     "crop buffer exceeds the platform address space");
        return LW_STATUS_OUT_OF_BOUNDS;
    }

    capacity = next_capacity(buffer->capacity, required_bytes);
    if (!crop_size_fits(capacity)) {
        capacity = required_bytes;
    }
    resized = (uint8_t*)realloc(buffer->data, (size_t)capacity);
    if (resized == NULL) {
        lw_set_error(error, LW_STATUS_OUT_OF_MEMORY,
                     "unable to grow OCR worker crop buffer");
        return LW_STATUS_OUT_OF_MEMORY;
    }
    buffer->data = resized;
    buffer->capacity = capacity;
    return LW_STATUS_OK;
}

void lw_crop_buffer_trim_after_request(lw_crop_buffer* buffer) {
    if (buffer == NULL || buffer->retained_limit == 0u ||
        buffer->capacity <= buffer->retained_limit) {
        return;
    }
    free(buffer->data);
    buffer->data = NULL;
    buffer->capacity = 0u;
}