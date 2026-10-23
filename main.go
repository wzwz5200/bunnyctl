package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// version is overridden at build time: -ldflags "-X main.version=1.2.3"
var version = "0.1.0-dev"

//go:embed web
var webFS embed.FS

var (
	dev      = &Device{}
	infoMu   sync.Mutex
	lastInfo *Info
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func statusCode(err error) int {
	if err == errNotConnected {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

type Status struct {
	Connected bool    `json:"connected"`
	Version   string  `json:"version"`
	Info      *Info   `json:"info,omitempty"`
	Preamp    *Preamp `json:"preamp,omitempty"`
	DAC       *EQ     `json:"dac,omitempty"`
	ADC       *EQ     `json:"adc,omitempty"`
	Path      string  `json:"path,omitempty"`
	Error     string  `json:"error,omitempty"`
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	st := Status{Version: version, Error: dev.LastError()}
	if info, err := dev.DeviceInfo(); err != nil {
		st.Connected = false
	} else {
		st.Connected = true
		st.Error = ""
		st.Info = &info
		st.Path = info.Path
		infoMu.Lock()
		lastInfo = &info
		infoMu.Unlock()
		if p, err := dev.ReadPreamp(); err == nil {
			st.Preamp = &p
		}
		if eq, err := dev.ReadEQ("dac"); err == nil {
			st.DAC = &eq
		}
		if eq, err := dev.ReadEQ("adc"); err == nil {
			st.ADC = &eq
		}
	}
	if st.Info == nil {
		infoMu.Lock()
		st.Info = lastInfo
		infoMu.Unlock()
	}
	writeJSON(w, http.StatusOK, st)
}

func handleEQ(w http.ResponseWriter, r *http.Request) {
	bank := strings.TrimPrefix(r.URL.Path, "/api/eq/")
	if _, ok := bankMap[bank]; !ok {
		errJSON(w, http.StatusBadRequest, errStr("未知通道"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		eq, err := dev.ReadEQ(bank)
		if err != nil {
			errJSON(w, statusCode(err), err)
			return
		}
		writeJSON(w, http.StatusOK, eq)
	case http.MethodPut:
		var eq EQ
		if err := json.NewDecoder(r.Body).Decode(&eq); err != nil {
			errJSON(w, http.StatusBadRequest, err)
			return
		}
		eq.Bank = bank
		if err := dev.WriteEQ(eq); err != nil {
			errJSON(w, statusCode(err), err)
			return
		}
		got, err := dev.ReadEQ(bank)
		if err != nil {
			errJSON(w, statusCode(err), err)
			return
		}
		writeJSON(w, http.StatusOK, got)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type errStr string

func (e errStr) Error() string { return string(e) }

func handlePreamp(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		p, err := dev.ReadPreamp()
		if err != nil {
			errJSON(w, statusCode(err), err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodPut:
		var p Preamp
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			errJSON(w, http.StatusBadRequest, err)
			return
		}
		if err := dev.WritePreamp(p); err != nil {
			errJSON(w, statusCode(err), err)
			return
		}
		got, _ := dev.ReadPreamp()
		writeJSON(w, http.StatusOK, got)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := dev.Save(); err != nil {
		errJSON(w, statusCode(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "saved_at": time.Now().Format(time.RFC3339)})
}

func handleFactory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := dev.WriteEQ(factoryDAC); err != nil {
		errJSON(w, statusCode(err), err)
		return
	}
	if err := dev.WriteEQ(factoryADC); err != nil {
		errJSON(w, statusCode(err), err)
		return
	}
	if err := dev.WritePreamp(Preamp{}); err != nil {
		errJSON(w, statusCode(err), err)
		return
	}
	if err := dev.Save(); err != nil {
		errJSON(w, statusCode(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "factory": factoryDAC})
}

func handleRegisters(w http.ResponseWriter, r *http.Request) {
	regs, err := dev.DumpAll()
	if err != nil {
		errJSON(w, statusCode(err), err)
		return
	}
	writeJSON(w, http.StatusOK, regs)
}

type rawReq struct {
	Addr  string `json:"addr"`
	Value uint32 `json:"value"`
}

func handleRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req rawReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(req.Addr), "0x"), 16, 8)
	if err != nil {
		errJSON(w, http.StatusBadRequest, errStr("地址格式应为 0x24 这类十六进制"))
		return
	}
	addr := uint8(n)
	if err := dev.Write(addr, req.Value); err != nil {
		errJSON(w, statusCode(err), err)
		return
	}
	v, _ := dev.Read(addr)
	writeJSON(w, http.StatusOK, map[string]any{"addr": req.Addr, "value": v})
}

func handlePresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, presets)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"version":   version,
		"connected": dev.Connected(),
	})
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}

func withLog(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h(w, r)
		if !strings.HasPrefix(r.URL.Path, "/api/status") {
			log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8099", "监听地址")
	showVersion := flag.Bool("version", false, "打印版本后退出")
	openUI := flag.Bool("open", false, "启动后自动打开浏览器")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", withLog(handleStatus))
	mux.HandleFunc("/api/eq/", withLog(handleEQ))
	mux.HandleFunc("/api/preamp", withLog(handlePreamp))
	mux.HandleFunc("/api/save", withLog(handleSave))
	mux.HandleFunc("/api/factory", withLog(handleFactory))
	mux.HandleFunc("/api/registers", withLog(handleRegisters))
	mux.HandleFunc("/api/raw", withLog(handleRaw))
	mux.HandleFunc("/api/presets", withLog(handlePresets))
	mux.HandleFunc("/api/health", withLog(handleHealth))
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		log.Fatal(err)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(index)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	url := "http://" + strings.Replace(*addr, "0.0.0.0", "127.0.0.1", 1)
	log.Printf("bunny-dsp %s — %s", version, url)
	if *openUI {
		go func() {
			time.Sleep(400 * time.Millisecond)
			openBrowser(url)
		}()
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
