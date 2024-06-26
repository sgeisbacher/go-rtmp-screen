package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/bep/debounce"
	"github.com/sgeisbacher/go-rtmp-screen/ringBuffer"
	"github.com/sgeisbacher/go-rtmp-screen/ui"
	"github.com/sgeisbacher/go-rtmp-screen/utils"
	webrtcutils "github.com/sgeisbacher/go-rtmp-screen/webrtc-utils"
)

const MAX_BUF_SECS = 30
const FRAME_RATE = 30

func main() {
	bufferCapDebouncer := debounce.New(1 * time.Second)
	initialdelaySecs := loadDelaySecs()
	desiredCapacity := initialdelaySecs * FRAME_RATE
	buffer := ringBuffer.CreateRingBuffer(desiredCapacity)
	videoTrackProvider := &webrtcutils.TrackProvider{}

	fmt.Printf("initial delaySecs: %d\n", initialdelaySecs)
	fmt.Printf("local ip: %s\n", utils.GetOutboundIP())

	http.Handle("/", templ.Handler(ui.PlayerLayout()))
	http.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		ui.AdminHomePage(desiredCapacity/FRAME_RATE, MAX_BUF_SECS).Render(r.Context(), w)
	})
	http.HandleFunc("POST /admin/rb/inc/{value}", func(w http.ResponseWriter, r *http.Request) {
		increaseSecs, err := strconv.Atoi(r.PathValue("value"))
		if err != nil {
			fmt.Printf("E: invalid increase value: %s", r.PathValue("value"))
			w.WriteHeader(500)
			return
		}
		newCap := desiredCapacity + increaseSecs*FRAME_RATE
		if toSecs(newCap) > 0 && toSecs(newCap) <= MAX_BUF_SECS {
			desiredCapacity = newCap
			bufferCapDebouncer(func() {
				persistDelaySecs(toSecs(newCap))
				buffer.Reset(desiredCapacity)
			})
		}

		ui.RingBufferInfos(toSecs(desiredCapacity), toSecs(buffer.GetCapacity()), MAX_BUF_SECS).Render(r.Context(), w)
	})
	http.HandleFunc("GET /raw/status", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, buffer.Status())
	})
	http.HandleFunc("GET /raw/ip", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, utils.GetOutboundIP())
	})
	http.HandleFunc("GET /raw/delay", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, fmt.Sprintf("%d", toSecs(buffer.GetCapacity())))
	})
	http.HandleFunc("GET /overlay/status", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, renderStatus(buffer))
	})
	http.HandleFunc("GET /overlay/delay", func(w http.ResponseWriter, r *http.Request) {
		respStr := fmt.Sprintf("delay: %ds", toSecs(buffer.GetCapacity()))
		io.WriteString(w, respStr)
	})
	http.HandleFunc("GET /overlay/url", func(w http.ResponseWriter, r *http.Request) {
		respStr := fmt.Sprintf("stream-url: rtmp://%v/publish/fridge", utils.GetOutboundIP())
		io.WriteString(w, respStr)
	})
	http.HandleFunc("GET /admin/infobox/buffer", func(w http.ResponseWriter, r *http.Request) {
		desiredSecs := toSecs(desiredCapacity)
		actualSecs := toSecs(buffer.GetCapacity())
		ui.RingBufferInfos(desiredSecs, actualSecs, MAX_BUF_SECS).Render(r.Context(), w)
	})
	http.HandleFunc("GET /admin/infobox/status", func(w http.ResponseWriter, r *http.Request) {
		status := buffer.Status()
		ui.StatusInfos(status).Render(r.Context(), w)
	})
	http.HandleFunc("GET /admin/infobox/framerate", func(w http.ResponseWriter, r *http.Request) {
		_, frameRate := buffer.Stats()
		ui.FrameRateInfos(frameRate).Render(r.Context(), w)
	})
	http.HandleFunc("GET /admin/infobox/datarate", func(w http.ResponseWriter, r *http.Request) {
		dataRate, _ := buffer.Stats()
		ui.DataRateInfos(dataRate/1024).Render(r.Context(), w)
	})
	http.HandleFunc("/createPeerConnection", buildCreatePeerConnectionHandleFunc(videoTrackProvider))

	go startRTMPServer(videoTrackProvider, buffer)
	fmt.Println("Listening on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("unknown error: %v\n", err)
	}
}

func renderStatus(buffer *ringBuffer.RingBuffer) string {
	switch buffer.Status() {
	case "idle":
		return "idle<br/><i style=\"font-size:30px;\">please start streaming app on your phone!</i>"
	case "streaming":
		return ""
	case "buffering":
		framesLeft, _ := buffer.BufferingFramesLeft()
		secsLeft := framesLeft / FRAME_RATE
		return fmt.Sprintf("%s (%ds) ...", buffer.Status(), secsLeft)
	case "disconnected":
		return "disconnected!<br><i style=\"font-size:30px;\">please (re)start streaming app on phone!</i>"
	default:
		return buffer.Status()
	}
}

func toSecs(n int) int {
	return n / FRAME_RATE
}
