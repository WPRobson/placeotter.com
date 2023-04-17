package main

import (
	"bytes"
	"fmt"
	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth_chi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jellydator/ttlcache/v3"
	"github.com/nfnt/resize"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

var images [][]byte

var imageResponseCache *ttlcache.Cache[string, []byte]

var errorMessage = "%s is not a valid request path. Please make sure requested image is not larger than 6000x4000 and that the extension is one of the following: jpg, jpeg, png, gif"

func run() error {
	r := chi.NewRouter()
	limiter := tollbooth.NewLimiter(1, nil)
	limiter.SetIPLookups([]string{"RemoteAddr", "X-Forwarded-For", "X-Real-IP"})
	r.Use(tollbooth_chi.LimitHandler(limiter))

	htmlBytes, err := os.ReadFile("resources/index.html")
	if err != nil {
		return fmt.Errorf("could not read index.html: %w", err)
	}
	indexHTML := string(htmlBytes)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		render.HTML(w, r, indexHTML)
	})

	err = loadImages(&images)
	if err != nil {
		return err
	}

	imageResponseCache = ttlcache.New[string, []byte](
		ttlcache.WithTTL[string, []byte](30 * time.Minute),
	)

	go imageResponseCache.Start()

	r.Get("/{width}/{heightOrHeightAndExtension}", func(w http.ResponseWriter, r *http.Request) {
		var height int
		var extension, contentType string
		widthParam := chi.URLParam(r, "width")
		heightOrExtensionParam := chi.URLParam(r, "heightOrHeightAndExtension")

		width, err := strconv.Atoi(widthParam)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(fmt.Sprintf(errorMessage, r.URL.Path)))
			return
		}

		extension, height = getExtensionAndHeightFromURLParam(heightOrExtensionParam)
		if height == 0 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(fmt.Sprintf(errorMessage, r.URL.Path)))
			return
		}
		if extension == "" {
			extension = "jpg"
			height, err = strconv.Atoi(heightOrExtensionParam)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(fmt.Sprintf(errorMessage, r.URL.Path)))
				return
			}
		}
		switch extension {
		case "jpg":
			contentType = "image/jpeg"
		case "jpeg":
			contentType = "image/jpeg"
		case "png":
			contentType = "image/png"
		case "gif":
			contentType = "image/gif"
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if width > 6000 || height > 4000 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(fmt.Sprintf(errorMessage, r.URL.Path)))
			return
		}

		picture, err := getRandomOtterImage(width, height, extension)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Sorry. Something went wrong while processing image"))
			return
		}

		w.Header().Set("Content-Type", contentType)
		_, err = w.Write(picture)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	})

	return http.ListenAndServe(":8080", r)
}

func getExtensionAndHeightFromURLParam(urlParam string) (string, int) {
	var heightString string
	regexPattern := "\\.[a-z]+"
	r, _ := regexp.Compile(regexPattern)
	ext := r.FindString(urlParam)
	if ext != "" {
		heightString = urlParam[0 : len(urlParam)-len(ext)]
		ext = ext[1:]
	} else {
		heightString = urlParam
	}
	height, err := strconv.Atoi(heightString)
	if err != nil {
		return "", 0
	}
	return ext, height
}

func loadImages(array *[][]byte) error {
	entries, err := os.ReadDir("resources/6000_by_4000")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		img, err := os.ReadFile("resources/6000_by_4000/" + e.Name())
		if err != nil {
			return err
		}
		*array = append(*array, img)
	}
	return nil
}

func getRandomOtterImage(width, height int, extension string) ([]byte, error) {
	randomIndex := rand.Intn(len(images))
	image := images[randomIndex]
	var err error
	cacheKey := fmt.Sprintf("%d %d %s", width, height, extension)
	if imageResponseCache.Get(cacheKey) != nil {
		return imageResponseCache.Get(cacheKey).Value(), nil
	}

	a, err := jpeg.Decode(bytes.NewReader(image))
	if err != nil {
		return nil, err
	}

	resizedImage := resize.Resize(uint(width), uint(height), a, resize.NearestNeighbor)
	buf := new(bytes.Buffer)

	switch extension {
	case "png":
		err = png.Encode(buf, resizedImage)
	case "gif":
		err = gif.Encode(buf, resizedImage, nil)
	default:
		err = jpeg.Encode(buf, resizedImage, &jpeg.Options{Quality: 40})
	}

	if err != nil {
		return nil, err
	}

	if imageResponseCache.Get(cacheKey) == nil {
		imageResponseCache.Set(cacheKey, buf.Bytes(), ttlcache.DefaultTTL)
	}

	return buf.Bytes(), nil
}
