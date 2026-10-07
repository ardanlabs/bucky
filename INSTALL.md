# Installing whisper.cpp libraries for bucky

bucky loads `whisper.cpp` at runtime via [purego](https://github.com/ebitengine/purego)
and [jupiterrider/ffi](https://github.com/JupiterRider/ffi) — there is **no
CGo** in this repository. That means you need a prebuilt shared library on
disk before any FFI call will succeed.

Set `BUCKY_LIB` (or pass `-lib <path>`) to the directory that contains the
shared library. The expected filenames are:

| OS              | Filename                                  |
| --------------- | ----------------------------------------- |
| linux / freebsd | `libwhisper.so`                           |
| darwin          | `libwhisper.dylib`                        |
| windows         | `whisper.dll` (plus `ggml*.dll` siblings) |

## macOS (arm64 / amd64)

```
make build
./bucky install -lib ./lib

# Authenticate the publisher manifest before installing.
./bucky install -lib ./lib -version 'v1.9.5@sha256:b835b4214be7025620d5cc89147ecbe14bb30a21939abe3e5524fe18cc892ecb'
```

Every install verifies the selected archive against its release manifest before
extraction. When no version is supplied, Bucky also authenticates that manifest
with its built-in SHA-256 pin. `bucky-builder` publishes pins for other versions
with each release.

This downloads the macOS-only universal xcframework from
[`ardanlabs/bucky-builder`](https://github.com/ardanlabs/bucky-builder) and
extracts its `macos-arm64_x86_64` Mach-O dylib into
`./lib/libwhisper.dylib`. Metal acceleration is included.

## Windows (amd64)

```
make build
.\bucky.exe install -lib .\lib
```

This downloads `whisper-vX.Y.Z-bin-windows-cpu-x64.zip` and extracts its
DLLs (`whisper.dll`, `ggml.dll`, `ggml-base.dll`, and the CPU variants).

For CUDA 12.4 builds use `-p cuda12` (or the compatibility alias `-p cuda`); that downloads
`whisper-vX.Y.Z-bin-windows-cuda-x64.zip` instead. The CUDA runtime DLLs
are included, so only a compatible NVIDIA driver is required on the host.

> **Windows ABI verification.** `pkg/whisper.WhisperFullParams` is sized
> assuming LLP64 with 4-byte `int` and 8-byte `size_t`/pointer — exactly
> what MSVC produces on Windows amd64. The
> [`Windows`](.github/workflows/windows.yml) GitHub Actions job runs the
> full FFI smoke on every push: `bucky install`, `bucky model get tiny`,
> `go test -count=1 ./...` (which exercises `TestWhisperFullParamsSize`,
> `TestVadParamsSize`, `TestVadContextParamsSize`, and `TestFullWithState`
> against the real `whisper.dll`), and `examples/hello samples/jfk.wav`.
> Watch the badge in [README.md](./README.md) for regressions; if you see
> `unsafe.Sizeof(WhisperFullParams) = N, want 304` with N != 304 the
> `_padN` fields in `pkg/whisper/params.go` need adjustment for the
> Windows ABI.

## Linux (amd64 / arm64)

```
make build
./bucky install -lib ./lib
```

Linux libraries are produced by the
[`ardanlabs/bucky-builder`](https://github.com/ardanlabs/bucky-builder)
companion repo. The builder checks twice daily for new whisper.cpp tags and
publishes eight purpose-built Linux artifacts per release:

| Backend   | amd64                                         | arm64                                           |
| --------- | --------------------------------------------- | ----------------------------------------------- |
| CPU       | `whisper-vX.Y.Z-bin-ubuntu-cpu-x64.tar.gz`    | `whisper-vX.Y.Z-bin-ubuntu-cpu-arm64.tar.gz`    |
| CUDA 12.9 | `whisper-vX.Y.Z-bin-ubuntu-cuda-x64.tar.gz`   | `whisper-vX.Y.Z-bin-ubuntu-cuda-arm64.tar.gz`   |
| CUDA 13.0 | `whisper-vX.Y.Z-bin-ubuntu-cuda-13-x64.tar.gz` | `whisper-vX.Y.Z-bin-ubuntu-cuda-13-arm64.tar.gz` |
| Vulkan    | `whisper-vX.Y.Z-bin-ubuntu-vulkan-x64.tar.gz` | `whisper-vX.Y.Z-bin-ubuntu-vulkan-arm64.tar.gz` |

`bucky install` detects NVIDIA driver capability via `nvidia-smi`. On Linux,
CUDA capability 13 or newer selects `cuda13`; older or unknown versions
select `cuda12`. Without a detected NVIDIA driver, it selects CPU. Pass
`-p cuda12` or `-p cuda13` to override this selection. The existing `-p cuda`
option remains a CUDA 12 alias. CUDA 13 is Linux-only. Pass `-p vulkan` to
opt into Vulkan, or `-p cpu` to force CPU.

Driver capability does not indicate which user-space runtime libraries are
installed. Linux CUDA bundles require matching `libcudart.so.12` and
`libcublas.so.12`, or `libcudart.so.13` and `libcublas.so.13`, plus a compatible
NVIDIA driver. Both runtime majors can coexist. Keep Jetson Orin on CUDA 12
unless its JetPack/runtime supports CUDA 13.

To replace an existing installation with CUDA 13:

```sh
./bucky install -lib ./lib --processor cuda13 --upgrade
```

CUDA arm64 targets Jetson Orin (sm_87) and DGX Spark (sm_121). CUDA amd64
includes sm_75 and sm_80 PTX plus native sm_86 and sm_89 builds.

The tarball unpacks `libwhisper.so`, `libggml.so`, `libggml-base.so`,
`libggml-cpu.so`, and (for cuda / vulkan variants) `libggml-cuda.so` /
`libggml-vulkan.so` into `lib/`. RPATH is `$ORIGIN`, so the libraries are
able to find their bundled siblings regardless of where you point `BUCKY_LIB`.
System dependencies, including NVIDIA runtime libraries, are not bundled.

If you'd rather build whisper.cpp yourself:

```
git clone https://github.com/ggml-org/whisper.cpp.git
cd whisper.cpp
git checkout v1.9.5
cmake -B build -DBUILD_SHARED_LIBS=ON
cmake --build build --config Release -j$(nproc)
mkdir -p ../bucky/lib
cp build/src/libwhisper.so ../bucky/lib/
cp build/ggml/src/libggml*.so ../bucky/lib/
```
