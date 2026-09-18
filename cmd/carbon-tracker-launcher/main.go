//go:build windows

package main

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procEnableWindow     = user32.NewProc("EnableWindow")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procLoadImageW       = user32.NewProc("LoadImageW")
	procSetClassLongPtrW = user32.NewProc("SetClassLongPtrW")
	procGetSysColorBrush = user32.NewProc("GetSysColorBrush")
	procSetWindowRgn     = user32.NewProc("SetWindowRgn")

	procCreateFontW        = gdi32.NewProc("CreateFontW")
	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procSetBkColor         = gdi32.NewProc("SetBkColor")
	procSetTextColor       = gdi32.NewProc("SetTextColor")
	procSetBkMode          = gdi32.NewProc("SetBkMode")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procFillRgn            = gdi32.NewProc("FillRgn")
	procCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
	procDrawTextW          = user32.NewProc("DrawTextW")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procShellExecuteW    = shell32.NewProc("ShellExecuteW")
	procMessageBoxW      = user32.NewProc("MessageBoxW")
)

const (
	WS_OVERLAPPED     = 0x00000000
	WS_CAPTION        = 0x00C00000
	WS_SYSMENU        = 0x00080000
	WS_MINIMIZEBOX    = 0x00020000
	WS_VISIBLE        = 0x10000000
	WS_CHILD          = 0x40000000
	WS_TABSTOP        = 0x00010000
	WS_VSCROLL        = 0x00200000
	WS_BORDER         = 0x00800000
	ES_MULTILINE      = 0x0004
	ES_AUTOVSCROLL    = 0x0040
	ES_READONLY       = 0x0800
	BS_PUSHBUTTON     = 0x00000000
	BS_OWNERDRAW      = 0x0000000B
	SS_CENTER         = 0x00000001
	SS_CENTERIMAGE    = 0x00000200
	SW_SHOW           = 5
	WM_DESTROY        = 0x0002
	WM_COMMAND        = 0x0111
	WM_DRAWITEM       = 0x002B
	WM_GETFONT        = 0x0031
	WM_SETFONT        = 0x0030
	WM_CTLCOLORSTATIC = 0x0138
	WM_CTLCOLOREDIT   = 0x0133
	WM_APP            = 0x8000
	WM_APP_STATUS     = WM_APP + 1
	WM_APP_OUTPUT     = WM_APP + 2
	WM_APP_BUSY       = WM_APP + 3
	COLOR_WINDOW      = 5
	TRANSPARENT       = 1
	DT_CENTER         = 0x00000001
	DT_VCENTER        = 0x00000004
	DT_SINGLELINE     = 0x00000020
	IDC_ARROW         = 32512
	IMAGE_ICON        = 1
	LR_LOADFROMFILE   = 0x0010
	LR_DEFAULTSIZE    = 0x0040
	MB_YESNO          = 0x00000004
	MB_ICONQUESTION   = 0x00000020
	IDYES             = 6
	GCLP_HICON        = -14
	GCLP_HICONSM      = -34
)

const (
	ID_DOCKER  = 1000
	ID_INSTALL = 1001
	ID_START   = 1002
	ID_STOP    = 1003
	ID_OPEN    = 1004
	ID_LOGS    = 1005
	ID_FOLDER  = 1006
)

type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}
type RECT struct{ Left, Top, Right, Bottom int32 }
type DRAWITEMSTRUCT struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   syscall.Handle
	HDC        syscall.Handle
	RcItem     RECT
	ItemData   uintptr
}
type POINT struct{ X, Y int32 }
type MSG struct {
	Hwnd           syscall.Handle
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             POINT
	LPrivate       uint32
}

var (
	hwndMain, hwndStatus, hwndOutput syscall.Handle
	buttons                          []syscall.Handle
	whiteBrush, outputBrush          syscall.Handle
	uiMu                             sync.Mutex
	queuedStatus, queuedOutput       string
	busy                             bool
)

func u16(s string) *uint16     { return syscall.StringToUTF16Ptr(s) }
func rgb(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

func main() {
	// Win32 windows and their message loops must remain on the same OS thread.
	runtime.LockOSThread()
	hInst, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
	white, _, _ := procCreateSolidBrush.Call(rgb(255, 255, 255))
	outb, _, _ := procCreateSolidBrush.Call(rgb(248, 250, 248))
	whiteBrush = syscall.Handle(white)
	outputBrush = syscall.Handle(outb)
	className := u16("CarbonTrackerLauncherV43")
	wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), LpfnWndProc: syscall.NewCallback(wndProc), HInstance: syscall.Handle(hInst), HCursor: syscall.Handle(cursor), HbrBackground: whiteBrush, LpszClassName: className}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(u16("Sustainability Monitoring Hub - Carbon Tracker"))), WS_OVERLAPPED|WS_CAPTION|WS_SYSMENU|WS_MINIMIZEBOX, 200, 120, 930, 430, 0, 0, hInst, 0)
	hwndMain = syscall.Handle(hwnd)
	loadIcon()
	createControls(syscall.Handle(hInst))
	procShowWindow.Call(hwnd, SW_SHOW)
	procUpdateWindow.Call(hwnd)
	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func createControls(hInst syscall.Handle) {
	title := create("STATIC", "Sustainability Monitoring Hub", WS_CHILD|WS_VISIBLE|SS_CENTER|SS_CENTERIMAGE, 20, 15, 890, 50, 0, hInst)
	subtitle := create("STATIC", "Local Carbon Tracker Launcher", WS_CHILD|WS_VISIBLE|SS_CENTER|SS_CENTERIMAGE, 20, 65, 890, 28, 0, hInst)
	hwndStatus = create("STATIC", "Ready. Use Docker Desktop first if Docker is not installed.", WS_CHILD|WS_VISIBLE|SS_CENTER|SS_CENTERIMAGE, 20, 96, 890, 28, 0, hInst)
	fontTitle, _, _ := procCreateFontW.Call(30, 0, 0, 0, 700, 0, 0, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(u16("Segoe UI"))))
	fontNormal, _, _ := procCreateFontW.Call(18, 0, 0, 0, 400, 0, 0, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(u16("Segoe UI"))))
	fontStatus, _, _ := procCreateFontW.Call(18, 0, 0, 0, 700, 0, 0, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(u16("Segoe UI"))))
	procSendMessageW.Call(uintptr(title), WM_SETFONT, fontTitle, 1)
	procSendMessageW.Call(uintptr(subtitle), WM_SETFONT, fontNormal, 1)
	procSendMessageW.Call(uintptr(hwndStatus), WM_SETFONT, fontStatus, 1)
	x := 20
	defs := []struct {
		id   int
		text string
		w    int
	}{{ID_DOCKER, "Docker Desktop", 142}, {ID_INSTALL, "Install / Update", 132}, {ID_START, "Start", 72}, {ID_STOP, "Stop", 72}, {ID_OPEN, "Open Carbon Tracker", 175}, {ID_LOGS, "View Logs", 105}, {ID_FOLDER, "Open Folder", 120}}
	for _, d := range defs {
		b := create("BUTTON", d.text, WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, x, 136, d.w, 38, d.id, hInst)
		procSendMessageW.Call(uintptr(b), WM_SETFONT, fontNormal, 1)
		rgn, _, _ := procCreateRoundRectRgn.Call(0, 0, uintptr(d.w), 38, 16, 16)
		procSetWindowRgn.Call(uintptr(b), rgn, 1)
		buttons = append(buttons, b)
		x += d.w + 10
	}
	hwndOutput = create("EDIT", "Launcher output will appear here.", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY, 20, 190, 890, 180, 0, hInst)
	procSendMessageW.Call(uintptr(hwndOutput), WM_SETFONT, fontNormal, 1)
}

func create(cls, text string, style uintptr, x, y, w, h int, id int, hInst syscall.Handle) syscall.Handle {
	r, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16(cls))), uintptr(unsafe.Pointer(u16(text))), style, uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(hwndMain), uintptr(id), uintptr(hInst), 0)
	return syscall.Handle(r)
}

func wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(wParam & 0xffff)
		switch id {
		case ID_DOCKER:
			go operation("docker desktop", installUpdateDockerDesktop)
		case ID_INSTALL:
			go operation("install", installUpdate)
		case ID_START:
			go operation("start", startApp)
		case ID_STOP:
			go operation("stop", stopApp)
		case ID_OPEN:
			go openURL()
		case ID_LOGS:
			go operation("logs", viewLogs)
		case ID_FOLDER:
			go openFolder()
		}
		return 0
	case WM_DRAWITEM:
		dis := (*DRAWITEMSTRUCT)(unsafe.Pointer(lParam))
		drawButton(dis)
		return 1
	case WM_APP_STATUS:
		uiMu.Lock()
		s := queuedStatus
		uiMu.Unlock()
		procSetWindowTextW.Call(uintptr(hwndStatus), uintptr(unsafe.Pointer(u16(s))))
		return 0
	case WM_APP_OUTPUT:
		uiMu.Lock()
		s := queuedOutput
		uiMu.Unlock()
		procSetWindowTextW.Call(uintptr(hwndOutput), uintptr(unsafe.Pointer(u16(s))))
		return 0
	case WM_APP_BUSY:
		en := uintptr(1)
		if wParam != 0 {
			en = 0
		}
		for _, b := range buttons {
			procEnableWindow.Call(uintptr(b), en)
		}
		return 0
	case WM_CTLCOLORSTATIC:
		procSetBkColor.Call(wParam, rgb(255, 255, 255))
		procSetTextColor.Call(wParam, rgb(35, 48, 42))
		return uintptr(whiteBrush)
	case WM_CTLCOLOREDIT:
		procSetBkColor.Call(wParam, rgb(248, 250, 248))
		procSetTextColor.Call(wParam, rgb(30, 40, 34))
		return uintptr(outputBrush)
	case WM_DESTROY:
		user32.NewProc("PostQuitMessage").Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

func drawButton(dis *DRAWITEMSTRUCT) {
	if dis == nil {
		return
	}
	bg := rgb(232, 239, 234)
	fg := rgb(28, 48, 36)
	switch int(dis.CtlID) {
	case ID_DOCKER:
		bg = rgb(210, 226, 238)
		fg = rgb(31, 65, 89)
	case ID_START:
		bg = rgb(181, 224, 188)
		fg = rgb(24, 70, 35)
	case ID_STOP:
		bg = rgb(244, 199, 199)
		fg = rgb(102, 38, 38)
	case ID_INSTALL:
		bg = rgb(215, 229, 218)
	case ID_OPEN:
		bg = rgb(222, 235, 224)
	}
	brush, _, _ := procCreateSolidBrush.Call(bg)
	w := dis.RcItem.Right - dis.RcItem.Left
	h := dis.RcItem.Bottom - dis.RcItem.Top
	rgn, _, _ := procCreateRoundRectRgn.Call(uintptr(dis.RcItem.Left), uintptr(dis.RcItem.Top), uintptr(dis.RcItem.Right), uintptr(dis.RcItem.Bottom), 16, 16)
	procFillRgn.Call(uintptr(dis.HDC), rgn, brush)
	procDeleteObject.Call(rgn)
	procDeleteObject.Call(brush)
	procSetBkMode.Call(uintptr(dis.HDC), TRANSPARENT)
	procSetTextColor.Call(uintptr(dis.HDC), fg)
	f, _, _ := procSendMessageW.Call(uintptr(dis.HwndItem), WM_GETFONT, 0, 0)
	if f != 0 {
		old, _, _ := procSelectObject.Call(uintptr(dis.HDC), f)
		defer procSelectObject.Call(uintptr(dis.HDC), old)
	}
	text := ""
	switch int(dis.CtlID) {
	case ID_DOCKER:
		text = "Docker Desktop"
	case ID_INSTALL:
		text = "Install / Update"
	case ID_START:
		text = "Start"
	case ID_STOP:
		text = "Stop"
	case ID_OPEN:
		text = "Open Carbon Tracker"
	case ID_LOGS:
		text = "View Logs"
	case ID_FOLDER:
		text = "Open Folder"
	}
	rc := dis.RcItem
	procDrawTextW.Call(uintptr(dis.HDC), uintptr(unsafe.Pointer(u16(text))), uintptr(^uint32(0)), uintptr(unsafe.Pointer(&rc)), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	_ = w
	_ = h
}

func setStatus(s string) {
	uiMu.Lock()
	queuedStatus = s
	uiMu.Unlock()
	procPostMessageW.Call(uintptr(hwndMain), WM_APP_STATUS, 0, 0)
}
func setOutput(s string) {
	uiMu.Lock()
	queuedOutput = s
	uiMu.Unlock()
	procPostMessageW.Call(uintptr(hwndMain), WM_APP_OUTPUT, 0, 0)
}
func setBusy(v bool) {
	uiMu.Lock()
	if busy == v {
		uiMu.Unlock()
		return
	}
	busy = v
	uiMu.Unlock()
	var n uintptr
	if v {
		n = 1
	}
	procPostMessageW.Call(uintptr(hwndMain), WM_APP_BUSY, n, 0)
}
func operation(name string, fn func() error) {
	setBusy(true)
	setStatus("Working on " + strings.Title(name) + ". Please wait....")
	defer setBusy(false)
	if err := fn(); err != nil {
		setStatus(strings.Title(name) + " failed.")
		setOutput(err.Error())
	}
}

func appRoot() string {
	p := os.Getenv("LOCALAPPDATA")
	if p == "" {
		p = os.TempDir()
	}
	return filepath.Join(p, "SustainabilityMonitoringHub")
}
func storedPathFile() string      { return filepath.Join(appRoot(), "launcher-compose-path.txt") }
func installedCommitFile() string { return filepath.Join(appRoot(), "installed-commit.txt") }

func findCompose() (string, error) {
	// Reuse a stored path when the file still exists. Do not require Docker
	// validation here: an existing Compose file can be valid even when Docker
	// Desktop is stopped or environment interpolation is temporarily unavailable.
	if b, err := os.ReadFile(storedPathFile()); err == nil {
		p := strings.TrimSpace(string(b))
		if isComposeCandidate(p) {
			return p, nil
		}
	}

	roots := []string{
		appRoot(),
		filepath.Join(os.Getenv("USERPROFILE"), "SustainabilityMonitoringHub"),
		filepath.Join(os.Getenv("USERPROFILE"), "Sustainability-Monitoring-Hub"),
	}
	names := map[string]int{"docker-compose.yml": 0, "docker-compose.yaml": 1, "compose.yml": 2, "compose.yaml": 3}
	type candidate struct {
		p           string
		rank, depth int
		standard    bool
	}
	var c []candidate
	seen := map[string]bool{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		if st, e := os.Stat(root); e != nil || !st.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			depth := 0
			if rel != "." {
				depth = len(strings.Split(rel, string(os.PathSeparator)))
			}
			if d.IsDir() {
				n := strings.ToLower(d.Name())
				if depth > 10 || n == ".git" || n == "node_modules" || n == "__pycache__" || n == "venv" || n == ".venv" {
					return filepath.SkipDir
				}
				return nil
			}
			n := strings.ToLower(d.Name())
			rank, standard := names[n]
			if !standard && !(strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".yaml")) {
				return nil
			}
			if seen[strings.ToLower(p)] {
				return nil
			}
			seen[strings.ToLower(p)] = true
			if standard || looksLikeCompose(p) {
				if !standard {
					rank = 20
				}
				c = append(c, candidate{p: p, rank: rank, depth: depth, standard: standard})
			}
			return nil
		})
	}
	sort.Slice(c, func(i, j int) bool {
		if c[i].standard != c[j].standard {
			return c[i].standard
		}
		if c[i].depth != c[j].depth {
			return c[i].depth < c[j].depth
		}
		if c[i].rank != c[j].rank {
			return c[i].rank < c[j].rank
		}
		return len(c[i].p) < len(c[j].p)
	})
	if len(c) == 0 {
		return "", errors.New("No Docker Compose file was found under " + appRoot() + ". Open Folder and confirm the application files are inside this folder.")
	}
	p := c[0].p
	_ = os.MkdirAll(appRoot(), 0755)
	_ = os.WriteFile(storedPathFile(), []byte(p), 0644)
	return p, nil
}

func looksLikeCompose(p string) bool {
	b, e := os.ReadFile(p)
	if e != nil {
		return false
	}
	if len(b) > 1024*1024 {
		b = b[:1024*1024]
	}
	t := strings.ToLower(string(b))
	return strings.Contains(t, "\nservices:") || strings.HasPrefix(strings.TrimSpace(t), "services:")
}

func isComposeCandidate(p string) bool {
	if p == "" {
		return false
	}
	st, e := os.Stat(p)
	if e != nil || st.IsDir() {
		return false
	}
	n := strings.ToLower(filepath.Base(p))
	if n == "docker-compose.yml" || n == "docker-compose.yaml" || n == "compose.yml" || n == "compose.yaml" {
		return true
	}
	return (strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".yaml")) && looksLikeCompose(p)
}

func isCompose(p string) bool { return isComposeCandidate(p) }

type composeProject struct {
	Name        string `json:"Name"`
	ConfigFiles string `json:"ConfigFiles"`
}

func composePathsFromDocker() ([]string, error) {
	exe, prefix, e := composeCommand()
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	args := append(append([]string{}, prefix...), "ls", "-a", "--format", "json")
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = dockerCommandEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := cmd.CombinedOutput()
	if e != nil {
		return nil, fmt.Errorf("docker compose ls failed: %v: %s", e, string(out))
	}
	var projects []composeProject
	if e := json.Unmarshal(out, &projects); e != nil {
		return nil, e
	}
	var paths []string
	for _, pr := range projects {
		for _, p := range strings.Split(pr.ConfigFiles, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	return paths, nil
}

func validateCompose(p string) (string, error) {
	exe, prefix, e := composeCommand()
	if e != nil {
		return "", e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	args := append(append([]string{}, prefix...), "-f", p, "config", "--services")
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = dockerCommandEnv()
	cmd.Dir = filepath.Dir(p)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", errors.New("Docker validation timed out")
	}
	if e != nil {
		return "", fmt.Errorf("Docker rejected this file: %v: %s", e, strings.TrimSpace(string(out)))
	}
	services := strings.TrimSpace(string(out))
	if services == "" {
		return "", errors.New("Compose file defines no services")
	}
	return services, nil
}

func dockerExe() (string, error) {
	// Prefer Docker Desktop's own CLI over any stale or unrelated docker.exe on PATH.
	for _, d := range dockerBinDirs() {
		p := filepath.Join(d, "docker.exe")
		if st, e := os.Stat(p); e == nil && !st.IsDir() {
			return p, nil
		}
	}
	if p, e := exec.LookPath("docker.exe"); e == nil {
		return p, nil
	}
	return "", errors.New("Docker CLI was not found. Open Docker Desktop, wait until it reports Running, then try again.")
}

func dockerBinDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	candidates := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "DockerDesktop", "resources", "bin"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Docker", "Docker", "resources", "bin"),
		filepath.Join(os.Getenv("ProgramFiles"), "Docker", "Docker", "resources", "bin"),
		filepath.Join(os.Getenv("ProgramW6432"), "Docker", "Docker", "resources", "bin"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Docker", "resources", "bin"),
	}
	for _, d := range candidates {
		if d == "" {
			continue
		}
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			key := strings.ToLower(d)
			if !seen[key] {
				seen[key] = true
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

func dockerCommandEnv() []string {
	// Refresh PATH for every Docker child process. This fixes the common case where
	// Docker Desktop was installed after this launcher was already opened and the
	// launcher's inherited PATH therefore does not contain resources\\bin.
	env := os.Environ()
	currentPath := os.Getenv("PATH")
	parts := dockerBinDirs()
	if currentPath != "" {
		parts = append(parts, currentPath)
	}
	newPath := strings.Join(parts, string(os.PathListSeparator))
	pathFound := false
	for i, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			env[i] = "PATH=" + newPath
			pathFound = true
		}
		// Do not override DOCKER_CONFIG. Docker Desktop may use the user's normal
		// config to discover CLI plugins such as Compose. The credential helper is
		// made available by the refreshed PATH above.
	}
	if !pathFound {
		env = append(env, "PATH="+newPath)
	}
	return env
}

func composeCommand() (string, []string, error) {
	d, err := dockerExe()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, d, "compose", "version")
		cmd.Env = dockerCommandEnv()
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		err2 := cmd.Run()
		cancel()
		if err2 == nil {
			return d, []string{"compose"}, nil
		}
	}
	for _, dir := range dockerBinDirs() {
		p := filepath.Join(dir, "docker-compose.exe")
		if st, e := os.Stat(p); e == nil && !st.IsDir() {
			return p, nil, nil
		}
	}
	if p, e := exec.LookPath("docker-compose.exe"); e == nil {
		return p, nil, nil
	}
	return "", nil, errors.New("Docker Compose was not found. Start Docker Desktop and wait until it reports Running, then try again.")
}

func credentialHelperPath() string {
	for _, d := range dockerBinDirs() {
		p := filepath.Join(d, "docker-credential-desktop.exe")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func runDocker(timeout time.Duration, compose string, args ...string) (string, error) {
	exe, prefix, e := composeCommand()
	if e != nil {
		return "", e
	}
	a := append([]string{}, prefix...)
	a = append(a, "-f", compose)
	ov := filepath.Join(filepath.Dir(compose), "docker-compose.launcher.override.yml")
	if _, e := os.Stat(ov); e == nil {
		a = append(a, "-f", ov)
	}
	a = append(a, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, a...)
	cmd.Env = dockerCommandEnv()
	cmd.Dir = filepath.Dir(compose)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("Docker command timed out after %s", timeout)
	}
	if e != nil {
		return string(out), fmt.Errorf("%v\r\n%s", e, string(out))
	}
	return string(out), nil
}

func dockerDesktopInstalled() bool {
	candidates := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "DockerDesktop", "Docker Desktop.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Docker", "Docker", "Docker Desktop.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Docker", "Docker", "Docker Desktop.exe"),
		filepath.Join(os.Getenv("ProgramW6432"), "Docker", "Docker", "Docker Desktop.exe"),
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return true
		}
	}
	// A Docker Desktop CLI in one of the known Desktop resource directories is
	// also sufficient evidence that Desktop is installed, even if Windows has not
	// refreshed environment variables yet.
	for _, d := range dockerBinDirs() {
		p := filepath.Join(d, "docker.exe")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

func downloadDockerDesktopInstaller() (string, error) {
	setStatus("Downloading Docker Desktop. Please wait....")
	setOutput("Downloading the current Docker Desktop installer from Docker.\r\n\r\nPlease wait.... Do not close this launcher while the download is in progress.")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	url := "https://desktop.docker.com/win/main/amd64/Docker%20Desktop%20Installer.exe"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Carbon-Tracker-Windows-Launcher")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Docker Desktop download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Docker Desktop download returned HTTP %d", resp.StatusCode)
	}
	tmp, err := os.MkdirTemp("", "carbon-tracker-docker-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(tmp, "Docker Desktop Installer.exe")
	f, err := os.Create(path)
	if err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	_, copyErr := io.Copy(f, io.LimitReader(resp.Body, 2*1024*1024*1024))
	closeErr := f.Close()
	if copyErr != nil {
		os.RemoveAll(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		os.RemoveAll(tmp)
		return "", closeErr
	}
	return path, nil
}

func runDockerDesktopInstaller(installer string) error {
	setStatus("Installing Docker Desktop silently. Please wait....")
	setOutput("Docker Desktop is being installed with the recommended per-user defaults and WSL 2 backend.\r\n\r\nPlease wait.... This may take several minutes. Carbon Tracker will NOT be installed automatically.")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, installer, "install", "--user", "--quiet", "--accept-license", "--backend=wsl-2")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New("Docker Desktop installation timed out after 30 minutes")
	}
	if err != nil {
		return fmt.Errorf("Docker Desktop installer failed: %v\r\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func tryDockerDesktopCLIUpdate() (bool, string) {
	d, err := dockerExe()
	if err != nil {
		return false, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, d, "desktop", "update", "--quiet")
	cmd.Env = dockerCommandEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, strings.TrimSpace(string(out))
	}
	return false, strings.TrimSpace(string(out))
}

func installUpdateDockerDesktop() error {
	setStatus("Checking Docker Desktop. Please wait....")
	setOutput("Checking whether Docker Desktop is installed and whether it can be updated.\r\n\r\nPlease wait.... Carbon Tracker installation will not start from this button.")
	if dockerDesktopInstalled() {
		setStatus("Docker Desktop is already installed. Checking for updates. Please wait....")
		if ok, out := tryDockerDesktopCLIUpdate(); ok {
			setStatus("Docker Desktop update check completed.")
			msg := "Docker Desktop is already installed and its update check completed."
			if out != "" {
				msg += "\r\n\r\n" + out
			}
			msg += "\r\n\r\nCarbon Tracker was NOT installed or started.\r\n\r\nStart Docker Desktop and wait until it reports Running. Then click Install / Update to install or update Carbon Tracker."
			setOutput(msg)
			return nil
		}
		setStatus("Docker Desktop is already installed.")
		setOutput("Docker Desktop was detected, so it was NOT downloaded or reinstalled.\r\n\r\nThe automatic Docker Desktop update command was unavailable or did not complete successfully. You can use Docker Desktop's own update mechanism later.\r\n\r\nCarbon Tracker was NOT installed or started. Start Docker Desktop and wait until it reports Running, then click Install / Update for Carbon Tracker.")
		return nil
	}
	installer, err := downloadDockerDesktopInstaller()
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(installer))
	if err := runDockerDesktopInstaller(installer); err != nil {
		return err
	}
	setStatus("Docker Desktop installation/update completed.")
	setOutput("Docker Desktop installation/update completed successfully.\r\n\r\nCarbon Tracker was NOT installed or started.\r\n\r\nPlease start Docker Desktop and wait until it reports Running. Then return here and click Install / Update to install or update Carbon Tracker.\r\n\r\nThe launcher will automatically use Docker Desktop's installed CLI and credential-helper paths even if Windows has not refreshed PATH yet.")
	return nil
}

func installUpdate() error {
	setStatus("Checking Carbon Tracker installation. Please wait....")
	setOutput("Checking for an existing local installation. If none is found, a fresh copy will be installed. Please keep this window open; the first Docker build can take several minutes.")
	compose, findErr := findCompose()
	if findErr != nil {
		return freshInstall()
	}
	root := filepath.Dir(compose)
	setStatus("Existing installation found. Checking for updates. Please wait....")
	latest, err := latestGitHubCommit()
	if err != nil {
		return err
	}
	installed := readInstalledCommit(root)
	if installed != "" && strings.EqualFold(installed, latest) {
		setStatus("Carbon Tracker is up to date.")
		setOutput("Installed commit: " + shortSHA(installed) + "\r\nLatest commit:    " + shortSHA(latest) + "\r\n\r\nNo update is required.")
		return nil
	}
	currentLabel := "unknown"
	if installed != "" {
		currentLabel = shortSHA(installed)
	}
	setStatus("An update is available.")
	setOutput("Installed commit: " + currentLabel + "\r\nLatest commit:    " + shortSHA(latest) + "\r\n\r\nWaiting for confirmation.")
	if !confirmUpdate(currentLabel, shortSHA(latest)) {
		setStatus("Update available; no changes were made.")
		return nil
	}
	setStatus("Downloading the latest Carbon Tracker source. Please wait....")
	staging, err := downloadRepositoryArchive(latest)
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	setStatus("Applying the update. Please wait.... Keep this window open.")
	if err := copyRepositoryUpdate(staging, root); err != nil {
		return err
	}
	setStatus("Rebuilding the Docker application. Please wait.... This can take several minutes.")
	out, err := runDocker(20*time.Minute, compose, "up", "-d", "--build")
	if err != nil {
		return fmt.Errorf("The source was updated, but Docker rebuild failed.\r\n%s\r\n%v", out, err)
	}
	_ = os.MkdirAll(appRoot(), 0755)
	_ = os.WriteFile(installedCommitFile(), []byte(latest), 0644)
	_ = os.WriteFile(filepath.Join(root, ".launcher-installed-commit"), []byte(latest), 0644)
	setStatus("Carbon Tracker updated successfully.")
	setOutput("Updated from " + currentLabel + " to " + shortSHA(latest) + ".\r\n\r\nDocker rebuild completed. Existing database volumes and uploaded files were preserved.\r\n\r\n" + out)
	return nil
}

func randomHex(bytesN int) (string, error) {
	b := make([]byte, bytesN)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func freshInstall() error {
	setStatus("No existing installation found. Preparing a fresh install. Please wait....")
	setOutput(`Fresh installation selected.

1. Checking Docker Desktop
2. Downloading Carbon Tracker
3. Creating local MySQL configuration
4. Building containers
5. Starting Carbon Tracker

Please keep this launcher open. The first build can take several minutes.`)

	docker, err := dockerExe()
	if err != nil {
		return errors.New("Docker Desktop is required for a fresh installation. Install and start Docker Desktop, wait until it reports Running, then click Install / Update again.\r\n\r\n" + err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	cmd := exec.CommandContext(ctx, docker, "info")
	cmd.Env = dockerCommandEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, daemonErr := cmd.CombinedOutput()
	cancel()
	if daemonErr != nil {
		return fmt.Errorf("Docker Desktop is installed but the Docker engine is not ready. Open Docker Desktop, wait until it reports Running, then click Install / Update again.\r\n\r\n%s", strings.TrimSpace(string(out)))
	}
	setOutput("Docker Desktop is ready. The launcher is using an isolated Docker CLI configuration so a newly installed Docker credential helper is not required for public image pulls.\r\n\r\nDownloading Carbon Tracker next. Please wait....")

	setStatus("Downloading the latest Carbon Tracker source. Please wait....")
	latest, err := latestGitHubCommit()
	if err != nil {
		return err
	}
	staging, err := downloadRepositoryArchive(latest)
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	root := appRoot()
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	setStatus("Installing application files. Please wait....")
	if err := copyFreshRepository(staging, root); err != nil {
		return err
	}

	dbPass, err := randomHex(24)
	if err != nil {
		return err
	}
	secret, err := randomHex(32)
	if err != nil {
		return err
	}
	env := "ENVIRONMENT=development\r\n" +
		"SECRET_KEY=" + secret + "\r\n" +
		"APP_NAME=Sustainability Monitoring Hub\r\n" +
		"APP_VERSION=local\r\n" +
		"DB_HOST=db\r\nDB_NAME=ghg_emissions_calculator_db\r\nDB_USER=root\r\n" +
		"DB_PASSWORD=" + dbPass + "\r\nDB_PORT=3306\r\nDB_SSL_DISABLED=true\r\n" +
		"TZ=Asia/Kuala_Lumpur\r\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(env), 0600); err != nil {
		return err
	}

	dockerfile := `FROM python:3.11-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 TZ=Asia/Kuala_Lumpur
WORKDIR /app
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends tzdata gcc default-libmysqlclient-dev pkg-config && rm -rf /var/lib/apt/lists/*
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8501
CMD ["python","-m","streamlit","run","app/main.py","--server.address=0.0.0.0","--server.port=8501","--server.fileWatcherType=none"]
`
	if err := os.WriteFile(filepath.Join(root, "Dockerfile.launcher"), []byte(dockerfile), 0644); err != nil {
		return err
	}

	composeText := `services:
  db:
    image: mysql:8.4
    restart: unless-stopped
    environment:
      MYSQL_ROOT_PASSWORD: ${DB_PASSWORD}
      MYSQL_DATABASE: ${DB_NAME}
      TZ: Asia/Kuala_Lumpur
    volumes:
      - smh_mysql_data:/var/lib/mysql
    healthcheck:
      test: ["CMD-SHELL", "mysqladmin ping -h 127.0.0.1 -uroot -p$$MYSQL_ROOT_PASSWORD --silent"]
      interval: 5s
      timeout: 5s
      retries: 30
      start_period: 20s

  app:
    build:
      context: .      dockerfile: Dockerfile.launcher
    restart: unless-stopped
    env_file:
      - .env
    environment:
      DB_HOST: db
      TZ: Asia/Kuala_Lumpur
    ports:
      - "8501:8501"
    depends_on:
      db:
        condition: service_healthy
    command: >-
      sh -c "python scripts/setup_db.py &&
      python scripts/setup_ghg_factors.py &&
      python -m streamlit run app/main.py --server.address=0.0.0.0 --server.port=8501 --server.fileWatcherType=none"

volumes:
  smh_mysql_data:
`
	compose := filepath.Join(root, "docker-compose.launcher.yml")
	if err := os.WriteFile(compose, []byte(composeText), 0644); err != nil {
		return err
	}
	_ = os.WriteFile(storedPathFile(), []byte(compose), 0644)

	setStatus("Building Carbon Tracker. Please wait...")
	setOutput("Application files installed. Docker is now downloading/building the required images.\r\n\r\nThis is normally the longest step. Please keep this window open.")
	buildOut, err := runDocker(25*time.Minute, compose, "up", "-d", "--build")
	if err != nil {
		return fmt.Errorf("Fresh installation files were created, but Docker build/start failed.\r\n\r\n%s\r\n%v", buildOut, err)
	}

	_ = os.WriteFile(installedCommitFile(), []byte(latest), 0644)
	_ = os.WriteFile(filepath.Join(root, ".launcher-installed-commit"), []byte(latest), 0644)
	setStatus("Carbon Tracker installed successfully.")
	setOutput("Fresh installation completed.\r\nInstalled commit: " + shortSHA(latest) + "\r\nTime zone: Asia/Kuala_Lumpur (UTC+08:00)\r\n\r\nCarbon Tracker is starting at http://localhost:8501. The browser will open when the service is ready.\r\n\r\n" + buildOut)
	go waitAndOpen()
	return nil
}

func copyFreshRepository(source, destination string) error {
	return filepath.WalkDir(source, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(destination, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func latestGitHubCommit() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/msf4-0/Sustainability-Monitoring-Hub-202602/commits/main", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Carbon-Tracker-Windows-Launcher")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Update check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("GitHub update check returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var v struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	if len(v.SHA) < 7 {
		return "", errors.New("GitHub returned an invalid commit identifier")
	}
	return strings.TrimSpace(v.SHA), nil
}

func readInstalledCommit(root string) string {
	for _, p := range []string{installedCommitFile(), filepath.Join(root, ".launcher-installed-commit")} {
		if b, err := os.ReadFile(p); err == nil {
			s := strings.TrimSpace(string(b))
			if len(s) >= 7 {
				return s
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		if git, err := exec.LookPath("git.exe"); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, git, "-C", root, "rev-parse", "HEAD")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			if out, err := cmd.Output(); err == nil {
				return strings.TrimSpace(string(out))
			}
		}
	}
	return ""
}

func shortSHA(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func confirmUpdate(current, latest string) bool {
	text := "A newer Carbon Tracker version is available.\r\n\r\nInstalled: " + current + "\r\nLatest: " + latest + "\r\n\r\nDownload, apply, and rebuild Docker now?\r\n\r\nYour database volume and uploaded files will be preserved."
	r, _, _ := procMessageBoxW.Call(uintptr(hwndMain), uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16("Carbon Tracker Update"))), MB_YESNO|MB_ICONQUESTION)
	return r == IDYES
}

func downloadRepositoryArchive(commit string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	url := "https://codeload.github.com/msf4-0/Sustainability-Monitoring-Hub-202602/zip/" + commit
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Carbon-Tracker-Windows-Launcher")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Repository download returned HTTP %d", resp.StatusCode)
	}
	tmp, err := os.MkdirTemp("", "carbon-tracker-update-")
	if err != nil {
		return "", err
	}
	zipPath := filepath.Join(tmp, "update.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	_, copyErr := io.Copy(f, io.LimitReader(resp.Body, 500*1024*1024))
	closeErr := f.Close()
	if copyErr != nil {
		os.RemoveAll(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		os.RemoveAll(tmp)
		return "", closeErr
	}
	extractRoot := filepath.Join(tmp, "extracted")
	if err := unzipSafe(zipPath, extractRoot); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	entries, err := os.ReadDir(extractRoot)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		os.RemoveAll(tmp)
		return "", errors.New("Downloaded repository archive has an unexpected structure")
	}
	return filepath.Join(extractRoot, entries[0].Name()), nil
}

func unzipSafe(zipPath, destination string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	cleanDest, _ := filepath.Abs(destination)
	for _, f := range r.File {
		target := filepath.Join(destination, f.Name)
		absTarget, _ := filepath.Abs(target)
		if absTarget != cleanDest && !strings.HasPrefix(absTarget, cleanDest+string(os.PathSeparator)) {
			return errors.New("Unsafe path in downloaded archive")
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func copyRepositoryUpdate(source, destination string) error {
	preserveTop := map[string]bool{
		".env": true, "docker-compose.launcher.override.yml": true,
		"docker-compose.launcher.yml": true, "Dockerfile.launcher": true,
		"uploads": true, "data": true, "mysql-data": true, "mysql_data": true,
		"storage": true, "backups": true, ".git": true,
	}
	return filepath.WalkDir(source, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		first := strings.Split(rel, string(os.PathSeparator))[0]
		if preserveTop[first] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}
func startApp() error {
	p, e := findCompose()
	if e != nil {
		return e
	}
	setStatus("Starting Carbon Tracker. Please wait....")
	setOutput("Using:\r\n" + p + "\r\n\r\nStarting existing containers. No download, rebuild, or database reset will run.")
	out, e := runDocker(90*time.Second, p, "up", "-d", "--no-build")
	if e != nil {
		return e
	}
	setStatus("Containers started. Waiting briefly for Carbon Tracker...")
	if strings.TrimSpace(out) != "" {
		setOutput(out)
	}
	go waitAndOpen()
	return nil
}
func stopApp() error {
	p, e := findCompose()
	if e != nil {
		return e
	}
	setStatus("Stopping Carbon Tracker. Please wait....")
	out, e := runDocker(45*time.Second, p, "stop", "-t", "15")
	if e != nil {
		return e
	}
	setStatus("Carbon Tracker stopped. Data was preserved.")
	if strings.TrimSpace(out) != "" {
		setOutput(out)
	}
	return nil
}
func viewLogs() error {
	p, e := findCompose()
	if e != nil {
		return e
	}
	setStatus("Collecting Docker logs. Please wait....")
	out, e := runDocker(45*time.Second, p, "logs", "--no-color", "--tail", "300")
	if e != nil && strings.TrimSpace(out) == "" {
		return e
	}
	if strings.TrimSpace(out) == "" {
		out = "No Docker log output is currently available."
	}
	logPath := filepath.Join(appRoot(), "Carbon-Tracker-logs.txt")
	if e2 := os.WriteFile(logPath, []byte(out), 0644); e2 != nil {
		return e2
	}
	if e2 := shellOpen(logPath); e2 != nil {
		return e2
	}
	setStatus("Logs opened in Notepad.")
	setOutput("Log file:\r\n" + logPath)
	return nil
}
func waitAndOpen() {
	client := http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 20; i++ {
		r, e := client.Get("http://127.0.0.1:8501")
		if e == nil {
			r.Body.Close()
			if r.StatusCode < 500 {
				setStatus("Carbon Tracker is running.")
				openURL()
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	setStatus("Containers started. Carbon Tracker may still be warming up.")
}
func shellOpen(target string) error {
	verb := u16("open")
	file := u16(target)
	r, _, _ := procShellExecuteW.Call(uintptr(hwndMain), uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, SW_SHOW)
	if r <= 32 {
		return fmt.Errorf("Windows Shell could not open %s (code %d)", target, r)
	}
	return nil
}
func openURL() {
	if e := shellOpen("http://localhost:8501"); e != nil {
		setOutput("Open Carbon Tracker failed: " + e.Error())
	} else {
		setStatus("Browser open request sent.")
	}
}
func openFolder() {
	root := appRoot()
	if e := os.MkdirAll(root, 0755); e != nil {
		setOutput("Open Folder failed: " + e.Error())
		return
	}
	if e := shellOpen(root); e != nil {
		setOutput("Open Folder failed: " + e.Error())
	} else {
		setStatus("Installation folder opened.")
	}
}

func loadIcon() {
	// Resource ID 1 is embedded into the executable by tools/embed_icon.py.
	// Loading it from the module keeps the title-bar/taskbar icon self-contained.
	hInst, _, _ := procGetModuleHandleW.Call(0)
	h, _, _ := procLoadImageW.Call(hInst, 1, IMAGE_ICON, 0, 0, LR_DEFAULTSIZE)
	if h == 0 {
		// Development fallback for an unprocessed local build.
		exe, _ := os.Executable()
		p := filepath.Join(filepath.Dir(exe), "Carbon-Tracker.ico")
		if _, e := os.Stat(p); e == nil {
			h, _, _ = procLoadImageW.Call(0, uintptr(unsafe.Pointer(u16(p))), IMAGE_ICON, 0, 0, LR_LOADFROMFILE|LR_DEFAULTSIZE)
		}
	}
	if h != 0 {
		procSetClassLongPtrW.Call(uintptr(hwndMain), uintptr(^uintptr(13)), h)
		procSetClassLongPtrW.Call(uintptr(hwndMain), uintptr(^uintptr(33)), h)
	}
}

var _ = bufio.NewReader
var _ = shell32