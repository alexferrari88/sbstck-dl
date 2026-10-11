#include <stdlib.h>
#include <stdio.h>
#include <string.h>

#include "libheif/heif.h"

int decode(uint8_t *heic_in, int heic_in_size, int config_only, uint32_t *width, uint32_t *height,
    uint32_t *colorspace, uint32_t *chroma, uint32_t *is_premultiplied, uint8_t *out);

int decode(uint8_t *heic_in, int heic_in_size, int config_only, uint32_t *width, uint32_t *height,
    uint32_t *colorspace, uint32_t *chroma, uint32_t *is_premultiplied, uint8_t *out) {

    enum heif_filetype_result filetype_check = heif_check_filetype(heic_in, heic_in_size);
    if(filetype_check != heif_filetype_yes_supported) {
        return 0;
    }

    struct heif_error err;

    struct heif_context *context = heif_context_alloc();
    err = heif_context_read_from_memory_without_copy(context, heic_in, heic_in_size, NULL);
    if(err.code != heif_error_Ok) {
        heif_context_free(context);
        return 0;
    }

    heif_context_set_max_decoding_threads(context, 0);

    struct heif_image_handle *handle;
    err = heif_context_get_primary_image_handle(context, &handle);
    if(err.code != heif_error_Ok) {
        heif_context_free(context);
        return 0;
    }

    *width = (uint32_t)heif_image_handle_get_width(handle);
    *height = (uint32_t)heif_image_handle_get_height(handle);

    *is_premultiplied = (uint32_t)heif_image_handle_is_premultiplied_alpha(handle);

    err = heif_image_handle_get_preferred_decoding_colorspace(handle, colorspace, chroma);
    if(err.code != heif_error_Ok) {
        heif_context_free(context);
        heif_image_handle_release(handle);
        return 0;
    }

    if(*colorspace == heif_colorspace_undefined || *chroma == heif_chroma_undefined) {
        *colorspace = heif_colorspace_YCbCr;
        *chroma = heif_chroma_420;
    }
    if(*colorspace == heif_colorspace_RGB) {
        *chroma = heif_chroma_interleaved_RGBA;
    }

    if(config_only) {
        heif_context_free(context);
        heif_image_handle_release(handle);
        return 1;
    }

    struct heif_decoding_options* options = heif_decoding_options_alloc();
    options->convert_hdr_to_8bit = 1;

    struct heif_image *img;

    err = heif_decode_image(handle, &img, *colorspace, *chroma, options);
    if(err.code != heif_error_Ok) {
        heif_context_free(context);
        heif_image_handle_release(handle);
        return 0;
    }

    if(*colorspace == heif_colorspace_YCbCr) {
        int h = *height;
        int ch = 0;

        switch(*chroma) {
            case heif_chroma_420:
                ch = (h+1)/2;
                break;
            case heif_chroma_422:
                ch = h;
                break;
            case heif_chroma_444:
                ch = h;
                break;
            default:
                break;
        }

        int y_stride;
        int u_stride;

        const uint8_t *y = heif_image_get_plane_readonly(img, heif_channel_Y, &y_stride);
        const uint8_t *cb = heif_image_get_plane_readonly(img, heif_channel_Cb, &u_stride);
        const uint8_t *cr = heif_image_get_plane_readonly(img, heif_channel_Cr, NULL);

        int i0 = y_stride * h;
        int i1 = y_stride * h + u_stride*ch;

        memcpy(out, y, y_stride * h);
        memcpy(out + i0, cb, u_stride * ch);
        memcpy(out + i1, cr, u_stride * ch);
    } else if(*colorspace == heif_colorspace_monochrome){
        int stride;
        const uint8_t *image = heif_image_get_plane_readonly(img, heif_channel_Y, &stride);

        memcpy(out, image, *height * stride);
    } else {
        int stride;
        const uint8_t *image = heif_image_get_plane_readonly(img, heif_channel_interleaved, &stride);

        memcpy(out, image, *height * stride);
    }

    heif_decoding_options_free(options);
    heif_context_free(context);
    heif_image_handle_release(handle);
    return 1;
}

int __cxa_allocate_exception(int a) {
    return 0;
}

void __cxa_throw(int a, int b, int c) {
}

int pthread_create(int a, int b, int c, int d) {
    return 0;
}

int pthread_join(int a, int b) {
    return 0;
}

int pthread_mutex_init(int a, int b) {
    return 0;
}

int pthread_mutex_lock(int a) {
    return 0;
}

int pthread_mutex_unlock(int a) {
    return 0;
}

int pthread_mutex_destroy(int a) {
    return 0;
}

int pthread_cond_init(int a, int b) {
    return 0;
}

int pthread_cond_signal(int a) {
    return 0;
}

int pthread_cond_wait(int a, int b) {
    return 0;
}

int pthread_cond_broadcast(int a) {
    return 0;
}

int pthread_cond_timedwait(int a, int b, int c) {
    return 0;
}

int pthread_cond_destroy(int a) {
    return 0;
}
