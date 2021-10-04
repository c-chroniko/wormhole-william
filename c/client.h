# include <stdint.h>

typedef struct client_config {
    char *app_id;
    char *rendezvous_url;
    char *transit_relay_url;
    int32_t passphrase_length;
} client_config;

typedef int32_t (*callback)(void *ctx, void* value, int32_t err_code);
int32_t call_callback (void *ctx, callback cb, void *value, int32_t err_code);

typedef void (*init_fn)(void *data);
void call_init (init_fn init, void *data);

typedef struct {
  int32_t length;
  uint8_t *data;
} file_t;
