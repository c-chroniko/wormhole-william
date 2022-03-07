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

typedef struct {
  int32_t error_code;
  char *error_string;
  char *code;
} codegen_result_t;

typedef struct {
  int64_t transferred_bytes;
  int64_t total_bytes;
} progress_t;

typedef void (*callback)(void *ptr, result_t *result);
typedef void (*progress_callback)(void *ptr, progress_t *progress);

void call_callback(void *ptr, callback cb, result_t *result);
void update_progress(void *ptr, progress_callback cb, progress_t *progress);
void free_result(result_t *result);
void free_codegen_result(codegen_result_t *result);
#endif
