#include <stdint.h>

#ifndef CLIENT_INCLUDED
#define CLIENT_INCLUDED
typedef struct {
  char *app_id;
  char *rendezvous_url;
  char *transit_relay_url;
  int32_t passphrase_length;
} client_config;

typedef struct {
  int32_t length;
  uint8_t *data;
  char *file_name;
} file_t;

typedef struct {
  file_t *file;
  int32_t err_code;
  char *err_string;
  char *received_text;
} result_t;

typedef void (*callback)(void *ptr, result_t *result);
void call_callback(void *ptr, callback cb, result_t *result);
void free_result(result_t *result);
#endif
