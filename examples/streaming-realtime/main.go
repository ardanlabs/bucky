// streaming-realtime demonstrates true block-by-block streaming
// transcription: audio arrives in arbitrarily sized blocks at its native
// sample rate (as a microphone or capture device would deliver it), is
// resampled to 16 kHz on the fly with a stateful audio.Resampler, and is
// decoded one window at a time. The tail of each window's tokens is carried
// into the next via whisper.StringRefs.SetPromptTokens for continuity.
//
// Unlike the streaming example — which loads a whole 16 kHz clip and slides
// a window over it — this example never holds the full resampled signal at a
// fixed rate. It feeds the source to the Resampler in small blocks and lets
// the Resampler stitch the seams: Process across many blocks yields the same
// samples as resampling the whole signal at once, with no per-block
// discontinuity or cumulative drift on ratios like 44100 -> 16000.
//
// A real capture device would push blocks from a callback; here we read a
// file at its native rate and chop it into fixed-size blocks to stand in for
// that source.
//
// Usage:
//
//	BUCKY_LIB=./lib BUCKY_TEST_MODEL=$HOME/models/ggml-tiny.bin \
//	    go run ./examples/streaming-realtime samples/jfk.wav
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ardanlabs/bucky/pkg/audio"
	"github.com/ardanlabs/bucky/pkg/whisper"
)

func main() {
	var (
		blockSize  = flag.Int("block", 4096, "capture block size in native-rate samples")
		windowSec  = flag.Float64("window", 10.0, "decode window size in seconds (5-10 typical)")
		nPromptTok = flag.Int("prompt-tokens", 64, "max tail tokens to carry into the next window")
		threads    = flag.Int("threads", 4, "number of CPU threads")
	)
	flag.Parse()
	if flag.NArg() < 1 {
		log.Fatalf("usage: %s [flags] <audio-file>", os.Args[0])
	}
	audioPath := flag.Arg(0)

	libPath := os.Getenv("BUCKY_LIB")
	if libPath == "" {
		log.Fatal("BUCKY_LIB must point to the directory containing libwhisper")
	}
	modelPath := os.Getenv("BUCKY_TEST_MODEL")
	if modelPath == "" {
		log.Fatal("BUCKY_TEST_MODEL must point to a GGML whisper model")
	}

	if err := whisper.Load(libPath); err != nil {
		log.Fatalf("whisper.Load: %v", err)
	}
	if err := whisper.Init(libPath); err != nil {
		log.Fatalf("whisper.Init: %v", err)
	}

	cparams := whisper.ContextDefaultParams()
	ctx, err := whisper.InitFromFileWithParams(modelPath, cparams)
	if err != nil {
		log.Fatalf("InitFromFileWithParams: %v", err)
	}
	defer whisper.Free(ctx)

	source, srcRate, err := loadNativeMono(audioPath)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "source: %d samples @ %d Hz, feeding in %d-sample blocks\n",
		len(source), srcRate, *blockSize)

	text, err := streamDecode(ctx, source, srcRate, *blockSize, *windowSec, *nPromptTok, *threads)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(strings.TrimSpace(text))
}

// loadNativeMono decodes the file at its native sample rate (no resampling)
// and downmixes to mono. This stands in for a capture device delivering raw
// PCM at, say, 44.1 or 48 kHz.
func loadNativeMono(audioPath string) ([]float32, int, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil, 0, fmt.Errorf("open %s: %w", audioPath, err)
	}
	defer f.Close()

	samples, sampleRate, channels, err := audio.DecodeRaw(f)
	if err != nil {
		return nil, 0, fmt.Errorf("audio.DecodeRaw: %w", err)
	}
	return audio.DownmixToMono(samples, channels), sampleRate, nil
}

// streamDecode feeds the source signal to a Resampler in fixed-size blocks,
// accumulating 16 kHz output until a full decode window is available. Each
// window is decoded with whisper.Full and its tail tokens are carried into
// the next window as PromptTokens.
func streamDecode(ctx whisper.Context, source []float32, srcRate, blockSize int, windowSec float64, nPromptTok, threads int) (string, error) {
	rs := audio.NewResampler(srcRate, whisper.SampleRate)
	winSamples := int(windowSec * float64(whisper.SampleRate))

	var (
		out         strings.Builder
		pending     []float32       // resampled 16 kHz samples not yet decoded
		promptToks  []whisper.Token // tail tokens from the previous window
		windowIndex int
	)

	flush := func(chunk []float32) error {
		nextPrompt, err := decodeWindow(ctx, chunk, promptToks, threads, &out)
		if err != nil {
			return fmt.Errorf("window %d: %w", windowIndex, err)
		}
		if len(nextPrompt) > nPromptTok {
			nextPrompt = nextPrompt[len(nextPrompt)-nPromptTok:]
		}
		promptToks = nextPrompt
		windowIndex++
		return nil
	}

	for off := 0; off < len(source); off += blockSize {
		end := min(off+blockSize, len(source))

		// Resample this capture block into 16 kHz and buffer it.
		pending = append(pending, rs.Process(source[off:end])...)

		// Decode as many full windows as the buffer now holds.
		for len(pending) >= winSamples {
			if err := flush(pending[:winSamples]); err != nil {
				return "", err
			}
			pending = pending[winSamples:]
		}
	}

	// Decode the trailing partial window, if any.
	if len(pending) > 0 {
		if err := flush(pending); err != nil {
			return "", err
		}
	}

	return out.String(), nil
}

// decodeWindow runs whisper.Full on a single 16 kHz window, appends its text
// to out, and returns the window's tokens for use as the next window's
// PromptTokens.
//
// SetPromptTokens copies the previous window's tail tokens into a buffer
// owned by refs and hands whisper.cpp a const whisper_token * into it; the
// deferred refs.KeepAlive keeps that buffer reachable for the duration of
// the Full call. NoContext=1 ensures whisper does not also prepend its own
// previous-segment context.
func decodeWindow(ctx whisper.Context, chunk []float32, promptToks []whisper.Token, threads int, out *strings.Builder) ([]whisper.Token, error) {
	wparams := whisper.FullDefaultParams(whisper.SamplingGreedy)
	wparams.NThreads = int32(threads)
	wparams.PrintProgress = 0
	wparams.PrintRealtime = 0
	wparams.PrintTimestamps = 0
	wparams.NoTimestamps = 1
	wparams.SingleSegment = 1
	wparams.NoContext = 1 // we manage context explicitly via PromptTokens

	var refs whisper.StringRefs
	refs.SetPromptTokens(&wparams, promptToks)
	defer refs.KeepAlive()

	if err := whisper.Full(ctx, wparams, chunk); err != nil {
		return nil, err
	}

	var nextPrompt []whisper.Token
	for i := int32(0); i < whisper.FullNSegments(ctx); i++ {
		out.WriteString(whisper.FullGetSegmentText(ctx, i))
		for j := int32(0); j < whisper.FullNTokens(ctx, i); j++ {
			id := whisper.FullGetTokenID(ctx, i, j)
			if id == whisper.TokenNull {
				continue
			}
			nextPrompt = append(nextPrompt, id)
		}
	}
	return nextPrompt, nil
}
