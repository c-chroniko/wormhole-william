# include <stdint.h>

typedef struct client_config {
    char *app_id;
    char *rendezvous_url;
    char *transit_relay_url;
    int32_t passphrase_length;
} client_config;

typedef void (*callback)(void *ctx, void* value, int32_t err_code);
void call_callback (void *ctx, callback cb, void *value, int32_t err_code);