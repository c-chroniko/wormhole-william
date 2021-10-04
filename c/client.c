#include "client.h"

int32_t call_callback(void *ctx, callback cb, void *result, int32_t err_code) {
    return cb(ctx, result, err_code);
};

void call_init(init_fn init, void *data) {
    init(data);
};
