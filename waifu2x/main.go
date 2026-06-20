package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"time"

	"github.com/google/uuid"
	_ "golang.org/x/image/webp"
)

type Waifu2xNcnnModel string

// Accepted format: "jpg", "png", or "webp"
type Waifu2xNcnnOutFormat string

type Void struct{}

var ReqsLimiter = make(chan Void, 4)
var GPUsChan = make(chan int, 2)
var AvaliableGPUs = []int{0, 1}

const (
	ModelCUNet        = "/usr/share/waifu2x-ncnn-vulkan/models-cunet"
	ModelUpConv7Anime = "/usr/share/waifu2x-ncnn-vulkan/models-upconv_7_anime_style_art_rgb"
	ModelUpConv7Photo = "/usr/share/waifu2x-ncnn-vulkan/models-upconv_7_photo"
)

func alphaRemover(inPath, outPath string) error {
	cmd := exec.Command("/usr/bin/magick", inPath, "-background", "white", "-alpha", "remove", outPath)
	return cmd.Run()
}

func waifu2xNCNNCaller(scale, noise, gpu int, model Waifu2xNcnnModel, inPath, outPath string, outFormat Waifu2xNcnnOutFormat) error {
	args := []string{
		"-i", inPath,
		"-o", outPath,
		"-f", string(outFormat),
		"-m", string(model),
		"-s", strconv.Itoa(scale),
		"-n", strconv.Itoa(noise),
		"-g", strconv.Itoa(gpu),
	}
	cmd := exec.Command("/usr/bin/waifu2x-ncnn-vulkan", args...)
	return cmd.Run()
}

func fileExists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}

func waifu2xHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	defer func() { log.Println("Request", r.URL.String(), "processed in", time.Since(now)) }()
	<-ReqsLimiter
	defer func() { ReqsLimiter <- Void{} }()
	model := r.PathValue("model")
	if model != "cunet" && model != "upconv-anime" && model != "upconv-photo" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(http.StatusText(http.StatusBadRequest)))
		return
	}
	scale, err := strconv.Atoi(r.PathValue("scale"))
	if err != nil || (scale != 1 && scale != 2 && scale != 4 && scale != 8) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(http.StatusText(http.StatusBadRequest)))
		return
	}
	noise, err := strconv.Atoi(r.PathValue("noise"))
	if err != nil || (noise < -1 || noise > 3) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(http.StatusText(http.StatusBadRequest)))
		return
	}
	if scale == 1 && noise == -1 {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(http.StatusText(http.StatusBadRequest)))
		return
	}
	format := r.PathValue("format")
	switch format {
	case "png":
		w.Header().Set("Content-Type", "image/png")
	case "jpg", "jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
		format = "jpg" // Do NOT remove this!
	case "webp":
		w.Header().Set("Content-Type", "image/webp")
	default:
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(http.StatusText(http.StatusBadRequest)))
		return
	}
	// Max size is 8 MiB.
	reader := http.MaxBytesReader(w, r.Body, 8<<20)
	imgData, err := io.ReadAll(reader)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(http.StatusText(http.StatusRequestEntityTooLarge)))
		return
	}
	img, inFmt, err := image.Decode(bytes.NewReader(imgData))
	if err != nil || (inFmt != "png" && inFmt != "jpeg" && inFmt != "webp") {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(http.StatusText(http.StatusBadRequest)))
		return
	}
	if max(img.Bounds().Dx(), img.Bounds().Dy()) > 8000 {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(http.StatusText(http.StatusRequestEntityTooLarge)))
		return
	}

	// Give this task a UUID.
	var taskId, inFileName string
	for {
		taskId = uuid.NewString()
		// Temp files will be created in /dev/shm, which means they will be written to mem.
		inFileName = path.Join("/dev/shm", "waifu2x."+taskId+".in."+format)
		if !fileExists(inFileName) {
			break
		}
	}

	outFileName := path.Join("/dev/shm", "waifu2x."+taskId+".out."+format)

	f, err := os.Create(inFileName)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(http.StatusText(http.StatusInternalServerError)))
		return
	}
	defer func() { _ = f.Close() }()

	_, err = f.Write(imgData)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(http.StatusText(http.StatusInternalServerError)))
		return
	}
	_ = f.Close()

	w.Header().Set("X-Input-Pre-Processed", "false")

	var waifu2xModel Waifu2xNcnnModel
	switch model {
	case "cunet":
		waifu2xModel = ModelCUNet
		switch img.(type) {
		case *image.RGBA, *image.NRGBA, *image.RGBA64, *image.NRGBA64,
			*image.NYCbCrA, *image.Alpha, *image.Alpha16:
			tempFileName := inFileName + ".processed." + format
			err := alphaRemover(inFileName, tempFileName)
			if err != nil {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(http.StatusText(http.StatusInternalServerError)))
				return
			}
			err = os.Rename(tempFileName, inFileName)
			if err != nil {
				err = os.Remove(tempFileName)
				if err != nil {
					log.Println("Can not remove temp file ", tempFileName, "since error occurred:", err.Error())
				}
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(http.StatusText(http.StatusInternalServerError)))
				return
			}
			w.Header().Set("X-Input-Pre-Processed", "true")
		}
	case "upconv-anime":
		waifu2xModel = ModelUpConv7Anime
	case "upconv-photo":
		waifu2xModel = ModelUpConv7Photo
	default:
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(http.StatusText(http.StatusInternalServerError)))
		return
	}

	gpu := <-GPUsChan
	defer func() {
		GPUsChan <- gpu
	}()

	err = waifu2xNCNNCaller(scale, noise, gpu, waifu2xModel, inFileName, outFileName, Waifu2xNcnnOutFormat(format))

	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(http.StatusText(http.StatusInternalServerError)))
		return
	}
	defer func() {
		err := os.Remove(inFileName)
		if err != nil {
			log.Println("Can not remove temp input file", inFileName, "since error occurred:", err.Error())
		}
		err = os.Remove(outFileName)
		if err != nil {
			log.Println("Can not remove temp output file", outFileName, "since error occurred:", err.Error())
		}
	}()

	outF, err := os.Open(outFileName)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(http.StatusText(http.StatusInternalServerError)))
		return
	}
	defer func() { _ = outF.Close() }()

	// Err can be ignored safely.
	_, _ = io.Copy(w, outF)
}

func fillChans() {
	for range cap(ReqsLimiter) {
		ReqsLimiter <- Void{}
	}
	if cap(GPUsChan) != len(AvaliableGPUs) {
		panic("GPUs chan buf size and avaliable GPUs count mismatch")
	}
	for _, gpu := range AvaliableGPUs {
		GPUsChan <- gpu
	}
}

func main() {
	fmt.Println("Waifu2x NCNN Vulkan Backend")
	fmt.Println("Version: 0.1.0")
	if len(os.Args) != 2 {
		panic("unexpected args count")
	}
	fillChans()
	http.HandleFunc("POST /{model}/{scale}/{noise}/{format}", waifu2xHandler)
	err := http.ListenAndServe(os.Args[1], nil)
	panic(err)
}
