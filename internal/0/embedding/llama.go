package embedding

// #cgo pkg-config: llama
// #include <llama.h>
// #include <stdlib.h>
import "C"

import (
	"fmt"
	"math"
	"strings"
	"unsafe"
)

type Embedder struct {
	model    *C.struct_llama_model
	ctx      *C.struct_llama_context
	vocab    *C.struct_llama_vocab
	nEmbd    int
	nCtx     int
	truncate bool
	strategy batchStrategy
}

// batchStrategy abstracts the encode-vs-decode divergence between
// true encoder models (BERT-family — snowflake-arctic-embed,
// nomic-embed-text) and decoder-only LLM-as-encoder models
// (Qwen3-Embedding, e5-mistral). Calling llama_encode against a model
// with no encoder block null-derefs inside libllama, and decoder
// models additionally need their KV cache cleared between embeds so
// state from the prior call doesn't leak into the next. All other
// embedding code (tokenize, batch build, get-embeddings, L2
// normalize) is architecture-agnostic.
type batchStrategy interface {
	// PrepareForEmbed runs any per-call setup the strategy requires
	// before submitting a batch. Decoder-mode strategies clear the
	// KV cache here; encoder-mode strategies have nothing to do.
	PrepareForEmbed(ctx *C.struct_llama_context)

	// RunBatch submits a populated llama_batch to the model and
	// returns the C return code. Implementations dispatch to
	// llama_encode (true encoders) or llama_decode (decoder-only).
	RunBatch(ctx *C.struct_llama_context, batch C.struct_llama_batch) C.int32_t
}

// encoderStrategy handles true encoder architectures (BERT-family).
// llama_encode is stateless wrt the KV cache, so PrepareForEmbed has
// nothing to do.
type encoderStrategy struct{}

func (encoderStrategy) PrepareForEmbed(*C.struct_llama_context) {}

func (encoderStrategy) RunBatch(ctx *C.struct_llama_context, batch C.struct_llama_batch) C.int32_t {
	return C.llama_encode(ctx, batch)
}

// decoderStrategy handles decoder-only LLM-as-encoder architectures
// (Qwen3-Embedding, e5-mistral, etc.). Each llama_decode call writes
// to the KV cache; without resetting between embeds, position state
// from prior calls leaks into the next and produces "llama batch
// failed: -1" once the cache is exhausted. PrepareForEmbed clears
// the cache so each Embed call starts with a clean slate.
type decoderStrategy struct{}

func (decoderStrategy) PrepareForEmbed(ctx *C.struct_llama_context) {
	// data=false clears metadata only (positions, sequence ids).
	// The data buffers are reused; for the embedding workload that's
	// the cheaper and sufficient reset.
	C.llama_memory_clear(C.llama_get_memory(ctx), C.bool(false))
}

func (decoderStrategy) RunBatch(ctx *C.struct_llama_context, batch C.struct_llama_batch) C.int32_t {
	return C.llama_decode(ctx, batch)
}

// NewEmbedder loads a GGUF model and creates a llama context for
// embedding inference. nCtx <= 0 uses the historical default of 512.
// poolingType is one of "" (model default), "mean", "cls", "last";
// any other value returns an error. n_batch / n_ubatch are clamped
// to the same value as nCtx to keep the context-window invariants
// consistent. truncate controls Embed's behavior on texts that
// tokenize past nCtx: false rejects them with an error, true embeds
// only the first nCtx tokens.
func NewEmbedder(modelPath string, nCtx int, poolingType string, truncate bool) (*Embedder, error) {
	installLlamaLogRedirect()
	ensureBackendsLoaded()

	if nCtx <= 0 {
		nCtx = 512
	}

	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))

	// llama_model_load_from_file reports nil on failure with no detail;
	// the actual reason (unsupported GGUF version, unknown architecture,
	// tensor mismatch, allocation failure) is emitted to llama.cpp's log
	// callback. Capture that output around the load so the error carries
	// the reason instead of just the path.
	captureActive := startLlamaLogCapture()

	mp := C.llama_model_default_params()
	model := C.llama_model_load_from_file(cPath, mp)

	var loadLog string
	if captureActive {
		loadLog = stopLlamaLogCapture()
	}

	if model == nil {
		if reason := lastLlamaError(loadLog); reason != "" {
			return nil, fmt.Errorf("failed to load model %s: %s", modelPath, reason)
		}
		return nil, fmt.Errorf("failed to load model: %s", modelPath)
	}

	cp := C.llama_context_default_params()
	cp.n_ctx = C.uint32_t(nCtx)
	cp.n_batch = C.uint32_t(nCtx)
	cp.n_ubatch = C.uint32_t(nCtx)
	cp.n_seq_max = 256 // support multi-sequence batching
	cp.embeddings = true

	switch poolingType {
	case "":
		// Leave llama_context_default_params' default in place
		// (LLAMA_POOLING_TYPE_UNSPECIFIED = let the model decide).
	case "mean":
		cp.pooling_type = C.LLAMA_POOLING_TYPE_MEAN
	case "cls":
		cp.pooling_type = C.LLAMA_POOLING_TYPE_CLS
	case "last":
		cp.pooling_type = C.LLAMA_POOLING_TYPE_LAST
	default:
		C.llama_model_free(model)
		return nil, fmt.Errorf("unknown pooling type %q (want one of \"\", \"mean\", \"cls\", \"last\")", poolingType)
	}

	ctx := C.llama_init_from_model(model, cp)
	if ctx == nil {
		C.llama_model_free(model)
		return nil, fmt.Errorf("failed to create context")
	}

	vocab := C.llama_model_get_vocab(model)
	nEmbd := int(C.llama_model_n_embd(model))

	var strategy batchStrategy = decoderStrategy{}
	if bool(C.llama_model_has_encoder(model)) {
		strategy = encoderStrategy{}
	}

	return &Embedder{
		model:    model,
		ctx:      ctx,
		vocab:    vocab,
		nEmbd:    nEmbd,
		nCtx:     nCtx,
		truncate: truncate,
		strategy: strategy,
	}, nil
}

// lastLlamaError distills llama.cpp's captured load output into a
// single human-meaningful reason for an error message. It prefers the
// last line that looks like an error/failure diagnostic; failing that,
// the last non-empty line (the load typically aborts right after
// printing the cause). Returns "" when the capture is empty.
func lastLlamaError(captured string) string {
	if captured == "" {
		return ""
	}

	var lastNonEmpty, lastErr string
	for _, line := range strings.Split(captured, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lastNonEmpty = line
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "fail") || strings.Contains(lower, "unsupported") || strings.Contains(lower, "unknown") {
			lastErr = line
		}
	}

	if lastErr != "" {
		return lastErr
	}
	return lastNonEmpty
}

func (e *Embedder) Embed(text string) ([]float32, error) {
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	textLen := C.int(len(text))

	// Tokenize into a context-window-sized buffer: a negative return
	// (buffer too small) then doubles as the "text exceeds the context
	// window" signal, so the common and reject paths each pay a single
	// tokenize pass.
	maxTokens := e.nCtx
	tokens := make([]C.llama_token, maxTokens)

	nTokens := C.llama_tokenize(
		e.vocab,
		cText,
		textLen,
		&tokens[0],
		C.int(maxTokens),
		true, // add_special (BOS)
		true, // parse_special
	)

	if nTokens < 0 {
		// The text tokenizes past nCtx. Never submit such a batch:
		// more tokens than n_ubatch (= nCtx) trips a GGML_ASSERT
		// inside llama.cpp ("encoder requires n_ubatch >= n_tokens")
		// that aborts the whole process instead of returning an error.
		if !e.truncate {
			return nil, fmt.Errorf("text tokenizes to %d tokens, exceeding context size %d", -nTokens, e.nCtx)
		}

		// Truncate: re-tokenize at the required size, then keep only
		// the first nCtx tokens. llama_tokenize does not fill the
		// buffer on overflow, so the retry is unavoidable. The dropped
		// tail includes any tokenizer-appended trailing special token.
		maxTokens = int(-nTokens)
		tokens = make([]C.llama_token, maxTokens)
		nTokens = C.llama_tokenize(
			e.vocab,
			cText,
			textLen,
			&tokens[0],
			C.int(maxTokens),
			true,
			true,
		)
		if nTokens < 0 {
			return nil, fmt.Errorf("tokenization failed")
		}
		if int(nTokens) > e.nCtx {
			nTokens = C.int(e.nCtx)
		}
	}

	// Use llama_batch_init so we can set seq_id and logits per token.
	// llama_batch_get_one does not populate these, which causes
	// llama_get_embeddings_seq to return zeros.
	batch := C.llama_batch_init(nTokens, 0, 1)
	defer C.llama_batch_free(batch)

	batch.n_tokens = nTokens
	tokenSlice := unsafe.Slice(batch.token, int(nTokens))
	posSlice := unsafe.Slice(batch.pos, int(nTokens))
	nSeqSlice := unsafe.Slice(batch.n_seq_id, int(nTokens))
	seqSlice := unsafe.Slice(batch.seq_id, int(nTokens))
	logitsSlice := unsafe.Slice(batch.logits, int(nTokens))

	for i := C.int(0); i < nTokens; i++ {
		tokenSlice[i] = tokens[i]
		posSlice[i] = C.llama_pos(i)
		nSeqSlice[i] = 1
		*seqSlice[i] = 0
		logitsSlice[i] = 0
	}
	// Mark last token for output
	logitsSlice[nTokens-1] = 1

	e.strategy.PrepareForEmbed(e.ctx)
	if ret := e.strategy.RunBatch(e.ctx, batch); ret != 0 {
		return nil, fmt.Errorf("llama batch failed: %d", ret)
	}

	// Use pooled sequence embedding for embedding models
	embPtr := C.llama_get_embeddings_seq(e.ctx, 0)
	if embPtr == nil {
		// Fall back to non-pooled embeddings
		embPtr = C.llama_get_embeddings(e.ctx)
	}
	if embPtr == nil {
		return nil, fmt.Errorf("llama_get_embeddings returned nil")
	}

	result := make([]float32, e.nEmbd)
	cSlice := unsafe.Slice(embPtr, e.nEmbd)
	for i := 0; i < e.nEmbd; i++ {
		result[i] = float32(cSlice[i])
	}

	// L2-normalize so cosine similarity works correctly
	var norm float64
	for _, v := range result {
		norm += float64(v) * float64(v)
	}
	if norm = math.Sqrt(norm); norm > 0 {
		for i := range result {
			result[i] = float32(float64(result[i]) / norm)
		}
	}

	return result, nil
}

// Tokenize returns the number of tokens in text without embedding it.
// Useful for estimating whether texts will fit in a batch.
func (e *Embedder) Tokenize(text string) (int, error) {
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))

	maxTokens := 512
	tokens := make([]C.llama_token, maxTokens)

	n := C.llama_tokenize(
		e.vocab, cText, C.int(len(text)),
		&tokens[0], C.int(maxTokens),
		true, true,
	)
	if n < 0 {
		// Negative means we need -n tokens; that's still a valid count.
		return int(-n), nil
	}
	return int(n), nil
}

// BatchEmbed embeds multiple texts in a single llama_encode call by
// assigning each text a separate seq_id. The total token count across
// all texts must not exceed the context size (512). Returns one
// L2-normalized embedding per input text, in the same order.
//
// Note: the truncate option is NOT applied here (except via the
// single-text fast path, which delegates to Embed) — a batch whose
// total exceeds the context window always errors. If BatchEmbed is
// ever wired into indexing, unify its overflow handling with Embed's
// truncate semantics first.
//
// Performance note: BERT-style embedding models use non-causal attention
// (every token attends to every other token in the batch). This means
// packing N sequences into one batch costs O((N*k)^2) attention rather
// than N * O(k^2), making batching slower than sequential calls for
// these models. BatchEmbed is provided for future use with causal models
// where batching does amortize the forward pass. For current BERT models
// (snowflake-arctic-embed, nomic-embed-text), prefer sequential Embed().
func (e *Embedder) BatchEmbed(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if len(texts) == 1 {
		vec, err := e.Embed(texts[0])
		if err != nil {
			return nil, err
		}
		return [][]float32{vec}, nil
	}

	// Tokenize each text independently.
	type tokenized struct {
		tokens []C.llama_token
		count  C.int
	}
	seqs := make([]tokenized, len(texts))
	var totalTokens C.int

	for i, text := range texts {
		cText := C.CString(text)
		maxTokens := 512
		tokens := make([]C.llama_token, maxTokens)

		n := C.llama_tokenize(
			e.vocab, cText, C.int(len(text)),
			&tokens[0], C.int(maxTokens),
			true, true,
		)
		C.free(unsafe.Pointer(cText))

		if n < 0 {
			maxTokens = int(-n)
			tokens = make([]C.llama_token, maxTokens)
			cText2 := C.CString(text)
			n = C.llama_tokenize(
				e.vocab, cText2, C.int(len(text)),
				&tokens[0], C.int(maxTokens),
				true, true,
			)
			C.free(unsafe.Pointer(cText2))
			if n < 0 {
				return nil, fmt.Errorf("tokenization failed for text %d", i)
			}
		}

		seqs[i] = tokenized{tokens: tokens[:n], count: n}
		totalTokens += n
	}

	if int(totalTokens) > e.nCtx {
		return nil, fmt.Errorf("batch total %d tokens exceeds context size %d", totalTokens, e.nCtx)
	}

	// Build batch with multiple sequences.
	batch := C.llama_batch_init(totalTokens, 0, C.int(len(texts)))
	defer C.llama_batch_free(batch)

	batch.n_tokens = totalTokens
	tokenSlice := unsafe.Slice(batch.token, int(totalTokens))
	posSlice := unsafe.Slice(batch.pos, int(totalTokens))
	nSeqSlice := unsafe.Slice(batch.n_seq_id, int(totalTokens))
	seqSlice := unsafe.Slice(batch.seq_id, int(totalTokens))
	logitsSlice := unsafe.Slice(batch.logits, int(totalTokens))

	idx := 0
	for seqID, seq := range seqs {
		for pos := C.int(0); pos < seq.count; pos++ {
			tokenSlice[idx] = seq.tokens[pos]
			posSlice[idx] = C.llama_pos(pos) // position resets per sequence
			nSeqSlice[idx] = 1
			*seqSlice[idx] = C.llama_seq_id(seqID)
			logitsSlice[idx] = 0
			idx++
		}
		// Mark last token of this sequence for output.
		logitsSlice[idx-1] = 1
	}

	e.strategy.PrepareForEmbed(e.ctx)
	if ret := e.strategy.RunBatch(e.ctx, batch); ret != 0 {
		return nil, fmt.Errorf("llama batch failed: %d", ret)
	}

	// Retrieve and normalize each sequence's embedding.
	results := make([][]float32, len(texts))
	for seqID := range texts {
		embPtr := C.llama_get_embeddings_seq(e.ctx, C.llama_seq_id(seqID))
		if embPtr == nil {
			return nil, fmt.Errorf("llama_get_embeddings_seq returned nil for seq %d", seqID)
		}

		result := make([]float32, e.nEmbd)
		cSlice := unsafe.Slice(embPtr, e.nEmbd)
		for i := 0; i < e.nEmbd; i++ {
			result[i] = float32(cSlice[i])
		}

		var norm float64
		for _, v := range result {
			norm += float64(v) * float64(v)
		}
		if norm = math.Sqrt(norm); norm > 0 {
			for i := range result {
				result[i] = float32(float64(result[i]) / norm)
			}
		}

		results[seqID] = result
	}

	return results, nil
}

// ContextSize returns the context window size (max tokens per batch)
// configured at NewEmbedder time.
func (e *Embedder) ContextSize() int {
	return e.nCtx
}

// TrainedContextSize returns the context size the model was trained
// with, from GGUF metadata (llama_model_n_ctx_train). A configured
// nCtx above this degrades embedding quality silently; callers can
// compare against ContextSize and warn.
func (e *Embedder) TrainedContextSize() int {
	return int(C.llama_model_n_ctx_train(e.model))
}

func (e *Embedder) EmbeddingDim() int {
	return e.nEmbd
}

func (e *Embedder) Close() {
	if e.ctx != nil {
		C.llama_free(e.ctx)
		e.ctx = nil
	}
	if e.model != nil {
		C.llama_model_free(e.model)
		e.model = nil
	}
}
