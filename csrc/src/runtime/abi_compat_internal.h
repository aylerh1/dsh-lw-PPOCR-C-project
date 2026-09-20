#ifndef LW_ABI_COMPAT_INTERNAL_H
#define LW_ABI_COMPAT_INTERNAL_H

/*
 * Helpers for additive ABI evolution. A caller advertises the number of
 * bytes available through the leading struct_size field. Output APIs copy
 * only the common prefix, ignore a newer caller's unknown tail, and preserve
 * the caller's advertised size in the returned structure.
 */

#include <stddef.h>
#include <stdint.h>
#include <string.h>

static inline int lw_abi_copy_output_prefix(void* destination, uint32_t supplied_size,
                                            const void* source, size_t current_size) {
    size_t copy_size;
    if (destination == NULL || source == NULL || supplied_size < sizeof(uint32_t)) {
        return 0;
    }
    copy_size = (size_t)supplied_size;
    if (copy_size > current_size) {
        copy_size = current_size;
    }
    memcpy(destination, source, copy_size);
    memcpy(destination, &supplied_size, sizeof(supplied_size));
    return 1;
}

static inline int lw_abi_copy_input_prefix(void* destination, size_t current_size,
                                           const void* source) {
    uint32_t supplied_size;
    size_t copy_size;
    if (destination == NULL || source == NULL) {
        return 0;
    }
    memcpy(&supplied_size, source, sizeof(supplied_size));
    if (supplied_size < sizeof(uint32_t)) {
        return 0;
    }
    copy_size = (size_t)supplied_size;
    if (copy_size > current_size) {
        copy_size = current_size;
    }
    memcpy(destination, source, copy_size);
    return 1;
}

static inline int lw_abi_copy_nested_input_prefix(void* destination, size_t current_size,
                                                  const void* source, size_t available_size) {
    uint32_t supplied_size;
    size_t copy_size;
    if (destination == NULL || source == NULL) {
        return 0;
    }
    if (available_size < sizeof(uint32_t)) {
        return 1;
    }
    memcpy(&supplied_size, source, sizeof(supplied_size));
    if (supplied_size < sizeof(uint32_t)) {
        return 0;
    }
    copy_size = (size_t)supplied_size;
    if (copy_size > available_size) {
        copy_size = available_size;
    }
    if (copy_size > current_size) {
        copy_size = current_size;
    }
    memcpy(destination, source, copy_size);
    return 1;
}

#endif
