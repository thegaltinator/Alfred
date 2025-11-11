#ifndef ALFRED_LLAMA_H
#define ALFRED_LLAMA_H

#include <stdbool.h>
#include <stdint.h>
#include <stddef.h>

#ifndef LLAMA_API
#define LLAMA_API
#endif

#ifdef __cplusplus
extern "C" {
#endif

typedef struct llama_model llama_model;
typedef struct llama_context llama_context;
typedef struct llama_vocab llama_vocab;
typedef struct llama_memory_i * llama_memory_t;

typedef int32_t llama_token;
typedef int32_t llama_pos;
typedef int32_t llama_seq_id;
typedef int32_t ggml_type;

typedef void * ggml_backend_dev_t;
typedef void * ggml_backend_buffer_type_t;
typedef bool (*llama_progress_callback)(float progress, void * user_data);

enum llama_split_mode {
    LLAMA_SPLIT_MODE_NONE  = 0,
    LLAMA_SPLIT_MODE_LAYER = 1,
    LLAMA_SPLIT_MODE_ROW   = 2,
};

enum llama_rope_scaling_type {
    LLAMA_ROPE_SCALING_TYPE_UNSPECIFIED = -1,
    LLAMA_ROPE_SCALING_TYPE_NONE        = 0,
    LLAMA_ROPE_SCALING_TYPE_LINEAR      = 1,
    LLAMA_ROPE_SCALING_TYPE_YARN        = 2,
    LLAMA_ROPE_SCALING_TYPE_LONGROPE    = 3,
};

enum llama_pooling_type {
    LLAMA_POOLING_TYPE_UNSPECIFIED = -1,
    LLAMA_POOLING_TYPE_NONE = 0,
    LLAMA_POOLING_TYPE_MEAN = 1,
    LLAMA_POOLING_TYPE_CLS  = 2,
    LLAMA_POOLING_TYPE_LAST = 3,
    LLAMA_POOLING_TYPE_RANK = 4,
};

enum llama_attention_type {
    LLAMA_ATTENTION_TYPE_UNSPECIFIED = -1,
    LLAMA_ATTENTION_TYPE_CAUSAL      = 0,
    LLAMA_ATTENTION_TYPE_NON_CAUSAL  = 1,
};

enum llama_flash_attn_type {
    LLAMA_FLASH_ATTN_TYPE_AUTO     = -1,
    LLAMA_FLASH_ATTN_TYPE_DISABLED = 0,
    LLAMA_FLASH_ATTN_TYPE_ENABLED  = 1,
};

struct llama_model_params {
    ggml_backend_dev_t * devices;
    const void * tensor_buft_overrides;
    int32_t n_gpu_layers;
    enum llama_split_mode split_mode;
    int32_t main_gpu;
    const float * tensor_split;
    llama_progress_callback progress_callback;
    void * progress_callback_user_data;
    const void * kv_overrides;
    bool vocab_only;
    bool use_mmap;
    bool use_mlock;
    bool check_tensors;
    bool use_extra_bufts;
    bool no_host;
};

struct llama_context_params {
    uint32_t n_ctx;
    uint32_t n_batch;
    uint32_t n_ubatch;
    uint32_t n_seq_max;
    int32_t  n_threads;
    int32_t  n_threads_batch;
    enum llama_rope_scaling_type rope_scaling_type;
    enum llama_pooling_type pooling_type;
    enum llama_attention_type attention_type;
    enum llama_flash_attn_type flash_attn_type;
    float rope_freq_base;
    float rope_freq_scale;
    float yarn_ext_factor;
    float yarn_attn_factor;
    float yarn_beta_fast;
    float yarn_beta_slow;
    uint32_t yarn_orig_ctx;
    float defrag_thold;
    void * cb_eval;
    void * cb_eval_user_data;
    ggml_type type_k;
    ggml_type type_v;
    void * abort_callback;
    void * abort_callback_data;
    bool embeddings;
    bool offload_kqv;
    bool no_perf;
    bool op_offload;
    bool swa_full;
    bool kv_unified;
};

typedef struct llama_batch {
    int32_t n_tokens;
    llama_token * token;
    float * embd;
    llama_pos * pos;
    int32_t * n_seq_id;
    llama_seq_id ** seq_id;
    int8_t * logits;
} llama_batch;

LLAMA_API void llama_backend_init(void);
LLAMA_API struct llama_model * llama_model_load_from_file(const char * path_model, struct llama_model_params params);
LLAMA_API void llama_model_free(struct llama_model * model);
LLAMA_API struct llama_context * llama_init_from_model(struct llama_model * model, struct llama_context_params params);
LLAMA_API void llama_free(struct llama_context * ctx);
LLAMA_API int32_t llama_model_n_embd(const struct llama_model * model);
LLAMA_API const struct llama_vocab * llama_model_get_vocab(const struct llama_model * model);
LLAMA_API enum llama_pooling_type llama_pooling_type(const struct llama_context * ctx);
LLAMA_API llama_memory_t llama_get_memory(const struct llama_context * ctx);
LLAMA_API void llama_memory_clear(llama_memory_t mem, bool data);
LLAMA_API void llama_set_embeddings(struct llama_context * ctx, bool embeddings);
LLAMA_API int32_t llama_tokenize(
        const struct llama_vocab * vocab,
        const char * text,
        int32_t text_len,
        llama_token * tokens,
        int32_t n_tokens_max,
        bool add_special,
        bool parse_special);
LLAMA_API struct llama_batch llama_batch_get_one(llama_token * tokens, int32_t n_tokens);
LLAMA_API int32_t llama_encode(struct llama_context * ctx, struct llama_batch batch);
LLAMA_API int32_t llama_decode(struct llama_context * ctx, struct llama_batch batch);
LLAMA_API float * llama_get_embeddings_seq(struct llama_context * ctx, llama_seq_id seq_id);
LLAMA_API float * llama_get_embeddings_ith(struct llama_context * ctx, int32_t i);
LLAMA_API void llama_synchronize(struct llama_context * ctx);
LLAMA_API int32_t llama_n_ctx(const struct llama_context * ctx);

#ifdef __cplusplus
}
#endif

#endif /* ALFRED_LLAMA_H */
