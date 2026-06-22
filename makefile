# Get the absolute path of the current Makefile.
MAKEFILE_PATH := $(realpath $(lastword $(MAKEFILE_LIST)))
MAKEFILE_DIR  := $(dir $(MAKEFILE_PATH))
BUCKY_LIB    ?= $(MAKEFILE_DIR)lib
MODELS_DIR   ?= $(HOME)/models

# make download-models fetches the GGML Whisper models used in tests/examples.
# Override MODELS_DIR=/path/to/models to put them somewhere else.
download-models:
	mkdir -p $(MODELS_DIR)
	curl -L -o $(MODELS_DIR)/ggml-tiny.bin   https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.bin
	curl -L -o $(MODELS_DIR)/ggml-base.en.bin https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin

clean-whisper.cpp:
	rm -rf $(BUCKY_LIB)/*

# make download-whisper.cpp VERSION=v1.9.1 to download a specific version.
download-whisper.cpp:
	go run . install -lib $(BUCKY_LIB) $(if $(VERSION),-v $(VERSION))

build:
	BUCKY_LIB=$(BUCKY_LIB) go build -o bucky .

install:
	go install .

lint:
	go vet ./...
	staticcheck -checks=all ./...

vuln-check:
	govulncheck ./...

diff:
	go fix -diff ./...

# make test runs all package tests. The pkg/whisper tests require
# BUCKY_LIB and BUCKY_TEST_MODEL/AUDIO to be set; without them they skip.
test-only:
	export BUCKY_LIB=$(BUCKY_LIB) && \
	export BUCKY_TEST_MODEL=$(MODELS_DIR)/ggml-tiny.bin && \
	export BUCKY_TEST_AUDIO=$(MAKEFILE_DIR)samples/jfk.wav && \
	go test -count=1 ./...

test: test-only lint vuln-check diff

tidy:
	go mod tidy

deps-upgrade:
	go get -u -v ./...
	go mod tidy

# ==============================================================================
# Profile and Benchmarks

# make bench runs BenchmarkFullJFK against BUCKY_BENCH_MODEL (defaults to
# ggml-tiny). Override BUCKY_BENCH_MODEL=$(MODELS_DIR)/ggml-base.en.bin to
# benchmark a larger model. Pass BENCHTIME=Nx to control iteration count.
BUCKY_BENCH_MODEL ?= $(MODELS_DIR)/ggml-tiny.bin
BENCHTIME         ?= 10x
bench:
	export BUCKY_LIB=$(BUCKY_LIB) && \
	export BUCKY_BENCH_MODEL=$(BUCKY_BENCH_MODEL) && \
	export BUCKY_TEST_AUDIO=$(MAKEFILE_DIR)samples/jfk.wav && \
	go test -bench=BenchmarkFullJFK -benchtime=$(BENCHTIME) -run='^$$' ./pkg/whisper/

# make profile-whisper captures CPU + memory profiles for BenchmarkFullJFK
# and writes them to ./profiles/. Useful for tracing time/allocs spent in
# purego trampolines, audio decode, etc. View with:
#
#   go tool pprof -http=:0 profiles/whisper.cpu.prof
#   go tool pprof -http=:0 profiles/whisper.mem.prof
#
# PROFILE_BENCHTIME is time-based (default 5s) so the CPU profile collects
# enough samples to be meaningful — pprof samples at 10ms granularity, so a
# benchmark that runs for only a few ms produces an empty profile. Use the
# `Ns` syntax (e.g. 100x) only if you specifically want a fixed iteration
# count.
PROFILE_BENCHTIME ?= 5s
profile-whisper:
	mkdir -p profiles
	export BUCKY_LIB=$(BUCKY_LIB) && \
	export BUCKY_BENCH_MODEL=$(BUCKY_BENCH_MODEL) && \
	export BUCKY_TEST_AUDIO=$(MAKEFILE_DIR)samples/jfk.wav && \
	go test -bench=BenchmarkFullJFK -benchtime=$(PROFILE_BENCHTIME) -run='^$$' \
	    -cpuprofile=profiles/whisper.cpu.prof \
	    -memprofile=profiles/whisper.mem.prof \
	    -benchmem \
	    -o profiles/whisper.test \
	    ./pkg/whisper/
	@echo
	@echo "Profiles written to ./profiles/. Inspect with:"
	@echo "  go tool pprof -http=:0 profiles/whisper.cpu.prof"
	@echo "  go tool pprof -http=:0 profiles/whisper.mem.prof"

# make profile-audio captures CPU + memory profiles for the pure-Go audio
# decode path. Useful for understanding allocation cost in DecodeWAV and
# friends. View the same way as profile-whisper.
profile-audio:
	mkdir -p profiles
	export BUCKY_TEST_AUDIO=$(MAKEFILE_DIR)samples/jfk.wav && \
	go test -bench=. -benchtime=$(PROFILE_BENCHTIME) -run='^$$' \
	    -cpuprofile=profiles/audio.cpu.prof \
	    -memprofile=profiles/audio.mem.prof \
	    -benchmem \
	    -o profiles/audio.test \
	    ./pkg/audio/
	@echo
	@echo "Profiles written to ./profiles/. Inspect with:"
	@echo "  go tool pprof -http=:0 profiles/audio.cpu.prof"
	@echo "  go tool pprof -http=:0 profiles/audio.mem.prof"

# make profile runs both profilers in sequence.
profile: profile-whisper profile-audio

# ==============================================================================
# Examples

# make hello runs the smallest possible bucky example end-to-end.
example-hello:
	export BUCKY_LIB=$(BUCKY_LIB) && \
	export BUCKY_TEST_MODEL=$(MODELS_DIR)/ggml-tiny.bin && \
	CGO_ENABLED=0 go run ./examples/hello samples/jfk.wav

example-segments:
	export BUCKY_LIB=$(BUCKY_LIB) && \
	export BUCKY_TEST_MODEL=$(MODELS_DIR)/ggml-tiny.bin && \
	CGO_ENABLED=0 go run ./examples/segments samples/jfk.wav

example-streaming:
	export BUCKY_LIB=$(BUCKY_LIB) && \
	export BUCKY_TEST_MODEL=$(MODELS_DIR)/ggml-tiny.bin && \
	CGO_ENABLED=0 go run ./examples/streaming samples/jfk.wav

example-transcribe:
	export BUCKY_LIB=$(BUCKY_LIB) && \
	export BUCKY_TEST_MODEL=$(MODELS_DIR)/ggml-tiny.bin && \
	CGO_ENABLED=0 go run ./examples/transcribe \
		-lang es \
		-prompt "Woman Talking" \
		samples/spanish.mp3

example-translate:
	export BUCKY_LIB=$(BUCKY_LIB) && \
	export BUCKY_TEST_MODEL=$(MODELS_DIR)/ggml-tiny.bin && \
	CGO_ENABLED=0 go run ./examples/translate \
		-lang es \
		samples/spanish.mp3

example-words:
	export BUCKY_LIB=$(BUCKY_LIB) && \
	export BUCKY_TEST_MODEL=$(MODELS_DIR)/ggml-tiny.bin && \
	CGO_ENABLED=0 go run ./examples/words samples/jfk.wav

# make example-diarize transcribes a stereo recording, labeling each channel
# as a distinct speaker (Speaker A on the left, Speaker B on the right).
example-diarize:
	export BUCKY_LIB=$(BUCKY_LIB) && \
	export BUCKY_TEST_MODEL=$(MODELS_DIR)/ggml-tiny.bin && \
	CGO_ENABLED=0 go run ./examples/diarize samples/stereo-speakers.wav
