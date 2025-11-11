import Foundation
import Darwin
import Accelerate
import CLlama

// MARK: - Errors

enum LlamaRuntimeError: Error, LocalizedError {
    case libraryNotFound([String])
    case symbolMissing(String)
    case modelLoadFailed(String)
    case contextInitializationFailed(String)
    case tokenizeFailed
    case inferenceFailed(Int32)
    case embeddingsUnavailable
    case normalizationFailed

    var errorDescription: String? {
        switch self {
        case .libraryNotFound(let attempts):
            return "libllama.dylib not found. Checked: \(attempts.joined(separator: ", "))"
        case .symbolMissing(let name):
            return "llama.cpp symbol missing: \(name)"
        case .modelLoadFailed(let reason):
            return "Failed to load Qwen model: \(reason)"
        case .contextInitializationFailed(let reason):
            return "Failed to create llama context: \(reason)"
        case .tokenizeFailed:
            return "Failed to tokenize input for embeddings"
        case .inferenceFailed(let status):
            return "llama_encode returned error \(status)"
        case .embeddingsUnavailable:
            return "llama_get_embeddings returned nil"
        case .normalizationFailed:
            return "Failed to normalize embedding vector"
        }
    }
}

// MARK: - Dynamic library loader

final class LlamaLibrary {
    private let handle: UnsafeMutableRawPointer

    init(path: String) throws {
        guard let handle = dlopen(path, RTLD_NOW | RTLD_LOCAL) else {
            throw LlamaRuntimeError.libraryNotFound([path])
        }
        self.handle = handle
    }

    deinit {
        dlclose(handle)
    }

    func loadSymbol<T>(_ name: String, as type: T.Type) throws -> T {
        guard let sym = dlsym(handle, name) else {
            throw LlamaRuntimeError.symbolMissing(name)
        }
        return unsafeBitCast(sym, to: type)
    }
}

// MARK: - Function table

struct LlamaFunctions {
    let llama_backend_init: @convention(c) () -> Void
    let llama_model_load_from_file: @convention(c) (UnsafePointer<CChar>?, llama_model_params) -> OpaquePointer?
    let llama_model_free: @convention(c) (OpaquePointer?) -> Void
    let llama_init_from_model: @convention(c) (OpaquePointer?, llama_context_params) -> OpaquePointer?
    let llama_free: @convention(c) (OpaquePointer?) -> Void
    let llama_model_n_embd: @convention(c) (OpaquePointer?) -> Int32
    let llama_model_get_vocab: @convention(c) (OpaquePointer?) -> OpaquePointer?
    let llama_pooling_type: @convention(c) (OpaquePointer?) -> llama_pooling_type
    let llama_get_memory: @convention(c) (OpaquePointer?) -> llama_memory_t
    let llama_memory_clear: @convention(c) (llama_memory_t, Bool) -> Void
    let llama_set_embeddings: @convention(c) (OpaquePointer?, Bool) -> Void
    let llama_tokenize: @convention(c) (OpaquePointer?, UnsafePointer<CChar>?, Int32, UnsafeMutablePointer<llama_token>?, Int32, Bool, Bool) -> Int32
    let llama_batch_get_one: @convention(c) (UnsafeMutablePointer<llama_token>?, Int32) -> llama_batch
    let llama_encode: @convention(c) (OpaquePointer?, llama_batch) -> Int32
    let llama_decode: @convention(c) (OpaquePointer?, llama_batch) -> Int32
    let llama_get_embeddings_seq: @convention(c) (OpaquePointer?, llama_seq_id) -> UnsafePointer<Float>?
    let llama_get_embeddings_ith: @convention(c) (OpaquePointer?, Int32) -> UnsafePointer<Float>?
    let llama_synchronize: @convention(c) (OpaquePointer?) -> Void
    let llama_n_ctx: @convention(c) (OpaquePointer?) -> Int32

    init(library: LlamaLibrary) throws {
        self.llama_backend_init = try library.loadSymbol("llama_backend_init", as: (@convention(c) () -> Void).self)
        self.llama_model_load_from_file = try library.loadSymbol("llama_model_load_from_file", as: (@convention(c) (UnsafePointer<CChar>?, llama_model_params) -> OpaquePointer?).self)
        self.llama_model_free = try library.loadSymbol("llama_model_free", as: (@convention(c) (OpaquePointer?) -> Void).self)
        self.llama_init_from_model = try library.loadSymbol("llama_init_from_model", as: (@convention(c) (OpaquePointer?, llama_context_params) -> OpaquePointer?).self)
        self.llama_free = try library.loadSymbol("llama_free", as: (@convention(c) (OpaquePointer?) -> Void).self)
        self.llama_model_n_embd = try library.loadSymbol("llama_model_n_embd", as: (@convention(c) (OpaquePointer?) -> Int32).self)
        self.llama_model_get_vocab = try library.loadSymbol("llama_model_get_vocab", as: (@convention(c) (OpaquePointer?) -> OpaquePointer?).self)
        self.llama_pooling_type = try library.loadSymbol("llama_pooling_type", as: (@convention(c) (OpaquePointer?) -> llama_pooling_type).self)
        self.llama_get_memory = try library.loadSymbol("llama_get_memory", as: (@convention(c) (OpaquePointer?) -> llama_memory_t).self)
        self.llama_memory_clear = try library.loadSymbol("llama_memory_clear", as: (@convention(c) (llama_memory_t, Bool) -> Void).self)
        self.llama_set_embeddings = try library.loadSymbol("llama_set_embeddings", as: (@convention(c) (OpaquePointer?, Bool) -> Void).self)
        self.llama_tokenize = try library.loadSymbol("llama_tokenize", as: (@convention(c) (OpaquePointer?, UnsafePointer<CChar>?, Int32, UnsafeMutablePointer<llama_token>?, Int32, Bool, Bool) -> Int32).self)
        self.llama_batch_get_one = try library.loadSymbol("llama_batch_get_one", as: (@convention(c) (UnsafeMutablePointer<llama_token>?, Int32) -> llama_batch).self)
        self.llama_encode = try library.loadSymbol("llama_encode", as: (@convention(c) (OpaquePointer?, llama_batch) -> Int32).self)
        self.llama_decode = try library.loadSymbol("llama_decode", as: (@convention(c) (OpaquePointer?, llama_batch) -> Int32).self)
        self.llama_get_embeddings_seq = try library.loadSymbol("llama_get_embeddings_seq", as: (@convention(c) (OpaquePointer?, llama_seq_id) -> UnsafePointer<Float>?).self)
        self.llama_get_embeddings_ith = try library.loadSymbol("llama_get_embeddings_ith", as: (@convention(c) (OpaquePointer?, Int32) -> UnsafePointer<Float>?).self)
        self.llama_synchronize = try library.loadSymbol("llama_synchronize", as: (@convention(c) (OpaquePointer?) -> Void).self)
        self.llama_n_ctx = try library.loadSymbol("llama_n_ctx", as: (@convention(c) (OpaquePointer?) -> Int32).self)
    }
}

// MARK: - Backend singleton

final class LlamaBackend {
    static let shared = LlamaBackend()

    private let lock = NSLock()
    private var initialized = false

    func ensureInitialized(functions: LlamaFunctions) {
        lock.lock()
        defer { lock.unlock() }
        if !initialized {
            functions.llama_backend_init()
            initialized = true
        }
    }
}

// MARK: - Session configuration

struct LlamaSessionConfiguration {
    let contextLength: Int
    let threadCount: Int
    let normalizeEmbeddings: Bool
    let pooling: llama_pooling_type
    let attention: llama_attention_type

    static func makeDefault() -> LlamaSessionConfiguration {
        let env = ProcessInfo.processInfo.environment
        let ctx = Int(env["ALFRED_EMBED_CTX"] ?? "") ?? 2048
        let threads = Int(env["ALFRED_EMBED_THREADS"] ?? "") ?? ProcessInfo.processInfo.activeProcessorCount
        let normalize = !(env["ALFRED_EMBED_DISABLE_NORMALIZE"] == "1")
        return LlamaSessionConfiguration(
            contextLength: max(512, ctx),
            threadCount: max(1, threads),
            normalizeEmbeddings: normalize,
            pooling: LLAMA_POOLING_TYPE_MEAN,
            attention: LLAMA_ATTENTION_TYPE_NON_CAUSAL
        )
    }
}

// MARK: - Llama session

final class LlamaSession {
    private let functions: LlamaFunctions
    private let library: LlamaLibrary
    private let model: OpaquePointer
    private let context: OpaquePointer
    private let vocab: OpaquePointer
    let embeddingDimension: Int
    let poolingType: llama_pooling_type
    let maxContextLength: Int
    let normalizeEmbeddings: Bool

    init(modelPath: String, libraryPath: String, configuration: LlamaSessionConfiguration = .makeDefault()) throws {
        self.library = try LlamaLibrary(path: libraryPath)
        self.functions = try LlamaFunctions(library: library)

        LlamaBackend.shared.ensureInitialized(functions: functions)

        var modelParams = LlamaSession.defaultModelParams()
        modelParams.n_gpu_layers = -1 // auto
        guard let modelPtr = LlamaSession.loadModel(path: modelPath, params: modelParams, functions: functions) else {
            throw LlamaRuntimeError.modelLoadFailed("llama_model_load_from_file returned NULL")
        }
        self.model = modelPtr

        guard let vocabPtr = functions.llama_model_get_vocab(modelPtr) else {
            functions.llama_model_free(modelPtr)
            throw LlamaRuntimeError.contextInitializationFailed("llama_model_get_vocab returned NULL")
        }

        let ctxParams = LlamaSession.defaultContextParams(configuration: configuration)

        guard let ctxPtr = functions.llama_init_from_model(modelPtr, ctxParams) else {
            functions.llama_model_free(modelPtr)
            throw LlamaRuntimeError.contextInitializationFailed("llama_init_from_model returned NULL")
        }
        self.context = ctxPtr

        self.vocab = vocabPtr
        self.functions.llama_set_embeddings(ctxPtr, true)
        self.poolingType = functions.llama_pooling_type(ctxPtr)
        self.embeddingDimension = Int(functions.llama_model_n_embd(modelPtr))
        self.maxContextLength = Int(functions.llama_n_ctx(ctxPtr))
        self.normalizeEmbeddings = configuration.normalizeEmbeddings

        print("✅ LlamaSession: model loaded (\(embeddingDimension)d, ctx=\(maxContextLength), threads=\(configuration.threadCount))")
    }
    
    private static func loadModel(path: String, params: llama_model_params, functions: LlamaFunctions) -> OpaquePointer? {
        return path.withCString { cString in
            functions.llama_model_load_from_file(cString, params)
        }
    }

    private static func defaultModelParams() -> llama_model_params {
        llama_model_params(
            devices: nil,
            tensor_buft_overrides: nil,
            n_gpu_layers: -1,
            split_mode: LLAMA_SPLIT_MODE_NONE,
            main_gpu: 0,
            tensor_split: nil,
            progress_callback: nil,
            progress_callback_user_data: nil,
            kv_overrides: nil,
            vocab_only: false,
            use_mmap: true,
            use_mlock: false,
            check_tensors: true,
            use_extra_bufts: false,
            no_host: false
        )
    }

    private static func defaultContextParams(configuration: LlamaSessionConfiguration) -> llama_context_params {
        let ctx = UInt32(configuration.contextLength)
        return llama_context_params(
            n_ctx: ctx,
            n_batch: ctx,
            n_ubatch: ctx,
            n_seq_max: 1,
            n_threads: Int32(configuration.threadCount),
            n_threads_batch: Int32(configuration.threadCount),
            rope_scaling_type: LLAMA_ROPE_SCALING_TYPE_UNSPECIFIED,
            pooling_type: configuration.pooling,
            attention_type: configuration.attention,
            flash_attn_type: LLAMA_FLASH_ATTN_TYPE_AUTO,
            rope_freq_base: 0,
            rope_freq_scale: 0,
            yarn_ext_factor: 0,
            yarn_attn_factor: 0,
            yarn_beta_fast: 0,
            yarn_beta_slow: 0,
            yarn_orig_ctx: 0,
            defrag_thold: 0,
            cb_eval: nil,
            cb_eval_user_data: nil,
            type_k: 0,
            type_v: 0,
            abort_callback: nil,
            abort_callback_data: nil,
            embeddings: true,
            offload_kqv: true,
            no_perf: false,
            op_offload: true,
            swa_full: true,
            kv_unified: true
        )
    }

    deinit {
        functions.llama_free(context)
        functions.llama_model_free(model)
    }

    func embed(text: String) throws -> [Float] {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            throw LlamaRuntimeError.tokenizeFailed
        }

        let utf8 = Array(trimmed.utf8)
        var tokens = [llama_token](repeating: 0, count: max(utf8.count + 16, 32))
        var produced: Int32 = 0

        while true {
            let result = utf8.withUnsafeBytes { rawBuffer -> Int32 in
                guard let textPtr = rawBuffer.bindMemory(to: CChar.self).baseAddress else {
                    return -1
                }

                return tokens.withUnsafeMutableBufferPointer { buffer -> Int32 in
                    functions.llama_tokenize(
                        vocab,
                        textPtr,
                        Int32(rawBuffer.count),
                        buffer.baseAddress,
                        Int32(buffer.count),
                        true,
                        true
                    )
                }
            }

            if result > 0 {
                produced = result
                break
            } else if result < 0 {
                let needed = Int(-result)
                tokens = [llama_token](repeating: 0, count: needed)
                continue
            } else {
                throw LlamaRuntimeError.tokenizeFailed
            }
        }

        if produced > Int32(maxContextLength) {
            produced = Int32(maxContextLength)
            tokens = Array(tokens.prefix(maxContextLength))
        } else {
            tokens.removeLast(tokens.count - Int(produced))
        }

        guard !tokens.isEmpty else {
            throw LlamaRuntimeError.tokenizeFailed
        }

        return try runInference(tokens: tokens)
    }

    private func runInference(tokens: [llama_token]) throws -> [Float] {
        functions.llama_memory_clear(functions.llama_get_memory(context), true)

        var mutableTokens = tokens
        let status = mutableTokens.withUnsafeMutableBufferPointer { buffer -> Int32 in
            let batch = functions.llama_batch_get_one(buffer.baseAddress, Int32(buffer.count))
            return functions.llama_decode(context, batch)
        }

        guard status == 0 else {
            throw LlamaRuntimeError.inferenceFailed(status)
        }

        functions.llama_synchronize(context)

        if poolingType != LLAMA_POOLING_TYPE_NONE,
           let seqPtr = functions.llama_get_embeddings_seq(context, 0) {
            return finalize(vectorPointer: seqPtr)
        }

        let lastIndex = Int32(tokens.count - 1)
        guard let tokenPtr = functions.llama_get_embeddings_ith(context, lastIndex) ?? functions.llama_get_embeddings_seq(context, 0) else {
            throw LlamaRuntimeError.embeddingsUnavailable
        }

        return finalize(vectorPointer: tokenPtr)
    }

    private func finalize(vectorPointer: UnsafePointer<Float>) -> [Float] {
        let buffer = UnsafeBufferPointer(start: vectorPointer, count: embeddingDimension)
        var vector = Array(buffer)
        if normalizeEmbeddings {
            normalize(&vector)
        }
        return vector
    }

    private func normalize(_ vector: inout [Float]) {
        guard !vector.isEmpty else { return }
        vector.withUnsafeMutableBufferPointer { buffer in
            guard let base = buffer.baseAddress else { return }
            var sum: Float = 0
            vDSP_dotpr(base, 1, base, 1, &sum, vDSP_Length(buffer.count))
            let length = sqrt(sum)
            guard length > 0 else { return }
            var inv = 1.0 / length
            vDSP_vsmul(base, 1, &inv, base, 1, vDSP_Length(buffer.count))
        }
    }
}
