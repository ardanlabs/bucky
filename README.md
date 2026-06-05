Copyright 2025-2026 Ardan Labs

hello@ardanlabs.com

# Bucky

This project lets you use Go for hardware accelerated local speech-to-text with [whisper.cpp](https://github.com/ggml-org/whisper.cpp) directly integrated into your applications. Bucky provides a high-level API that mirrors `whisper.h` 1-to-1 plus pure-Go audio decoding so you can hand any WAV/MP3/FLAC file to a model and get a transcript back.

Bucky is the speech-to-text sibling of [hybridgroup/yzma](https://github.com/hybridgroup/yzma) (which binds llama.cpp). The end goal is to give [Kronk](https://github.com/ardanlabs/kronk) a native, OpenAI-compatible `POST /v1/audio/transcriptions` endpoint without the CGo toolchain.

> Bucky is the squirrel from _The Emperor's New Groove_ — he speaks in
> squeaks/whispers. Naming a Whisper binding after him is just good taste.

To install bucky, fetch the whisper.cpp shared libraries, and transcribe the bundled JFK sample:

```shell
$ go install github.com/ardanlabs/bucky@latest

$ bucky install -lib ./lib
$ export BUCKY_LIB=$(pwd)/lib

$ bucky model get tiny
$ bucky whisper transcribe -m ~/models/ggml-tiny.bin samples/jfk.wav
```

Read [INSTALL.md](./INSTALL.md) for per-OS install notes and [MODELS.md](./MODELS.md) for the recommended whisper model set.

## Project Status

[![Go Reference](https://pkg.go.dev/badge/github.com/ardanlabs/bucky.svg)](https://pkg.go.dev/github.com/ardanlabs/bucky)
[![Go Report Card](https://goreportcard.com/badge/github.com/ardanlabs/bucky?style=flat-square)](https://goreportcard.com/report/github.com/ardanlabs/bucky)
[![go.mod Go version](https://img.shields.io/github/go-mod/go-version/ardanlabs/bucky)](https://github.com/ardanlabs/bucky)
[![whisper.cpp Release](https://img.shields.io/github/v/release/ggml-org/whisper.cpp?label=whisper.cpp)](https://github.com/ggml-org/whisper.cpp/releases)

[![Linux](https://github.com/ardanlabs/bucky/actions/workflows/linux.yml/badge.svg)](https://github.com/ardanlabs/bucky/actions/workflows/linux.yml)
[![macOS](https://github.com/ardanlabs/bucky/actions/workflows/macos.yml/badge.svg)](https://github.com/ardanlabs/bucky/actions/workflows/macos.yml)
[![Windows](https://github.com/ardanlabs/bucky/actions/workflows/windows.yml/badge.svg)](https://github.com/ardanlabs/bucky/actions/workflows/windows.yml)

Sometimes there are breaking changes to whisper.cpp that require an update to bucky. Here are the known compatible versions:

| whisper.cpp | bucky |
| ----------- | ----- |
| v1.8.6      | 0.1.x |

The core FFI binding (model loading, `whisper_full`, segments + tokens, VAD, state, language, bench helpers), audio decoding (WAV/MP3/FLAC), CLI (`install`, `system`, `model get|info|list`, `whisper transcribe`), and examples (`hello`, `transcribe`, `translate`, `segments`, `words`, `streaming`, `streaming-realtime`) have all landed. Kronk integration (an OpenAI-compatible `POST /v1/audio/transcriptions` endpoint) lives in the [kronk](https://github.com/ardanlabs/kronk) repo.

## Owner Information

```
Name:     Bill Kennedy
Company:  Ardan Labs
Title:    Managing Partner
Email:    bill@ardanlabs.com
BlueSky:  https://bsky.app/profile/goinggo.net
LinkedIn: www.linkedin.com/in/william-kennedy-5b318778/
Twitter:  https://x.com/goinggodotnet
```

## Install Bucky

The fastest way to install on any supported platform is with Go:

```shell
$ go install github.com/ardanlabs/bucky@latest

$ bucky --help
```

Then fetch the whisper.cpp shared library bundle (xcframework on darwin, DLLs on windows, `.tar.gz` from [ardanlabs/bucky-builder](https://github.com/ardanlabs/bucky-builder) on linux):

```shell
$ bucky install -lib ./lib
$ export BUCKY_LIB=$(pwd)/lib
$ bucky system
```

And pull a model from the bundled catalog:

```shell
$ bucky model list
$ bucky model get tiny
$ bucky model info -m ~/models/ggml-tiny.bin
```

## Issues/Features

Here is the existing [Issues/Features](https://github.com/ardanlabs/bucky/issues) for the project and the things being worked on or things that would be nice to have.

If you are interested in helping in any way, please send an email to [Bill Kennedy](mailto:bill@ardanlabs.com).

## Architecture

The architecture of bucky mirrors yzma file-for-file so anyone who knows yzma can drop straight in. There is no CGo: every C call goes through [purego](https://github.com/ebitengine/purego) + [JupiterRider/ffi](https://github.com/JupiterRider/ffi).

```
┌─────────────────────────────────────────────────────────────┐
│  cmd/         bucky CLI (install, system, model, whisper)   │
├─────────────────────────────────────────────────────────────┤
│  pkg/whisper  1-to-1 mirror of whisper.h                    │
│               (model, context, full, segments, tokens,      │
│                lang, state, vad, bench, params)             │
│  pkg/audio    pure-Go decoders (WAV / MP3 / FLAC) +         │
│               downmix-to-mono + 16 kHz resampling           │
│  pkg/loader   BUCKY_LIB-aware purego library loader         │
│  pkg/download go-getter-driven release-archive resolver     │
│  pkg/utils    cross-platform Go ↔ C string helpers          │
└─────────────────────────────────────────────────────────────┘
                          │
                          ▼
              libwhisper.{dylib|so|dll}
                  (whisper.cpp v1.8.6)
```

## Models

Bucky uses GGML-format models supported by whisper.cpp. The official set lives at [ggerganov/whisper.cpp on Hugging Face](https://huggingface.co/ggerganov/whisper.cpp) (tiny / base / small / medium / large-v3 / large-v3-turbo, plus `-en` English-only and quantized `-q5_0` / `-q8_0` variants). Companion VAD models live at [ggml-org/whisper-vad](https://huggingface.co/ggml-org/whisper-vad).

Bucky ships a small bundled catalog so you can `bucky model get tiny` instead of pasting a URL:

```shell
$ bucky model list
$ bucky model get tiny
$ bucky model get -u https://example.com/foo.bin -o ~/models   # arbitrary URL
$ bucky model get silero-vad
```

See [MODELS.md](./MODELS.md) for the recommended set with size / speed / quality trade-offs.

## Support

Bucky uses the prebuilt whisper.cpp release artifacts where they exist; Linux artifacts come from the [ardanlabs/bucky-builder](https://github.com/ardanlabs/bucky-builder) companion repo (whisper.cpp upstream publishes no Linux release).

| OS      | CPU          | GPU               | Source                                                                           |
| ------- | ------------ | ----------------- | -------------------------------------------------------------------------------- |
| Linux   | amd64, arm64 | CUDA 12.9, Vulkan | `whisper-vX.Y.Z-bin-ubuntu-{cpu,cuda,vulkan}-{x64,arm64}.tar.gz` (bucky-builder) |
| macOS   | arm64, amd64 | Metal             | `whisper-vX.Y.Z-xcframework.zip` (upstream)                                      |
| Windows | amd64        | CPU, CUDA 12      | `whisper-bin-x64.zip` / `-cublas-…` (upstream)                                   |

Whenever there is a new release of whisper.cpp, the FFI struct mirrors and `pkg/download` matrix may need a refresh. The pinned version is captured in `pkg/download/`.

## API Examples

There are examples in the [examples/](./examples) directory. Each one
expects `BUCKY_LIB` and `BUCKY_TEST_MODEL` to be set:

```shell
$ export BUCKY_LIB=$(pwd)/lib
$ export BUCKY_TEST_MODEL=$HOME/models/ggml-tiny.bin
```

[HELLO](examples/hello/main.go) — the smallest possible bucky program: load a tiny model, decode an audio file, print the transcript.

```shell
$ make hello
```

[TRANSCRIBE](examples/transcribe/main.go) — fuller transcription example with `-lang`, `-prompt`, `-temperature`, and `-beam` flags.

```shell
$ go run ./examples/transcribe -lang en samples/jfk.wav
```

[TRANSLATE](examples/translate/main.go) — sets `wparams.Translate = 1` to translate non-English audio to English.

```shell
$ go run ./examples/translate -m $HOME/models/ggml-base.bin some-foreign-audio.wav
```

[SEGMENTS](examples/segments/main.go) — print each generated segment with `[mm:ss.mmm -> mm:ss.mmm] text`. Pass `-tokens` for per-token detail.

```shell
$ go run ./examples/segments -tokens samples/jfk.wav
```

[WORDS](examples/words/main.go) — enable token-level timestamps and print `[t0 -> t1] word` for every emitted token. Documents the experimental DTW path in a comment.

```shell
$ go run ./examples/words samples/jfk.wav
```

[STREAMING](examples/streaming/main.go) — sliding-window pseudo-streaming over the input file. Each window's tail tokens are carried into the next via `whisper.StringRefs.SetPromptTokens`.

```shell
$ go run ./examples/streaming samples/jfk.wav
```

[STREAMING-REALTIME](examples/streaming-realtime/main.go) — true block-by-block streaming: native-rate audio is fed in fixed-size blocks through a stateful `audio.Resampler` (e.g. 44100 → 16000) and decoded one window at a time, again carrying context via `SetPromptTokens`.

```shell
$ go run ./examples/streaming-realtime samples/spanish.mp3
```

## Sample API Program — Hello Example

```go
// hello is the smallest possible bucky example: load a tiny whisper model,
// decode an audio file (WAV / MP3 / FLAC), and print the resulting text.
package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ardanlabs/bucky/pkg/audio"
	"github.com/ardanlabs/bucky/pkg/whisper"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: %s <audio-file>", os.Args[0])
	}
	audioPath := os.Args[1]

	libPath := os.Getenv("BUCKY_LIB")
	if libPath == "" {
		log.Fatal("BUCKY_LIB must point to the directory containing libwhisper")
	}
	modelPath := os.Getenv("BUCKY_TEST_MODEL")
	if modelPath == "" {
		log.Fatal("BUCKY_TEST_MODEL must point to a GGML whisper model (e.g. ggml-tiny.bin)")
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

	f, err := os.Open(audioPath)
	if err != nil {
		log.Fatalf("open %s: %v", audioPath, err)
	}
	defer f.Close()

	samples, err := audio.Decode(f)
	if err != nil {
		log.Fatalf("audio.Decode: %v", err)
	}

	wparams := whisper.FullDefaultParams(whisper.SamplingGreedy)
	wparams.PrintProgress = 0
	wparams.PrintRealtime = 0
	wparams.PrintTimestamps = 0
	wparams.NoTimestamps = 1

	if err := whisper.Full(ctx, wparams, samples); err != nil {
		log.Fatalf("Full: %v", err)
	}

	var sb strings.Builder
	for i := int32(0); i < whisper.FullNSegments(ctx); i++ {
		sb.WriteString(whisper.FullGetSegmentText(ctx, i))
	}
	fmt.Println(strings.TrimSpace(sb.String()))
}
```

This example produces the following output:

```shell
$ make hello
go run ./examples/hello samples/jfk.wav
And so my fellow Americans ask not what your country can do for you ask what you can do for your country.
```

## License

Apache-2.0 — see [LICENSE](./LICENSE).
