package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// --- Data Models ---

type Partition struct {
	PartUUID string `json:"partuuid"`
	Name     string `json:"name"`
	FSType   string `json:"fstype"`
}

type Disk struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Children []Partition `json:"children,omitempty"`
}

type LsblkOutput struct {
	BlockDevices []Disk `json:"blockdevices"`
}

type VitastorParameters struct {
	DataDevice                   string `json:"data_device"`
	BitmapGranularity            int    `json:"bitmap_granularity"`
	BlockSize                    int    `json:"block_size"`
	OSDNum                       int    `json:"osd_num"`
	DisableDataFsync             bool   `json:"disable_data_fsync"`
	DisableDeviceLock            bool   `json:"disable_device_lock"`
	ImmediateCommit              string `json:"immediate_commit"`
	DiskAlignment                int    `json:"disk_alignment"`
	JournalBlockSize             int    `json:"journal_block_size"`
	MetaBlockSize                int    `json:"meta_block_size"`
	JournalNoSameSectorOverwrites bool   `json:"journal_no_same_sector_overwrites"`
	JournalSectorBufferCount     int    `json:"journal_sector_buffer_count"`
}

type OSDPrepareParameters struct {
	Disk   string `json:"disk"`
	OSDNum *int   `json:"osd_num"`
}

// --- Helpers ---

// execShell executes a command. Note: explicitly not using a shell unless necessary for security.
// However, to mimic Python's shell=True string interpolation behavior, we construct args carefully.
func execShell(name string, args ...string) (int, string, string) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = -1 // Generic error (e.g. binary not found)
		}
	}

	return exitCode, stdout.String(), stderr.String()
}

func getSystemDisks(diskMask string) ([]Disk, error) {
	// lsblk -p -J -o NAME,PARTUUID,FSTYPE,TYPE {disk_mask}
	args := []string{"-p", "-J", "-o", "NAME,PARTUUID,FSTYPE,TYPE"}
	if diskMask != "" {
		args = append(args, diskMask)
	}

	code, stdout, stderr := execShell("lsblk", args...)
	if code != 0 {
		return nil, fmt.Errorf("lsblk failed: %s", stderr)
	}

	// Some lsblk versions might return empty JSON or no output if device not found
	if strings.TrimSpace(stdout) == "" {
		return []Disk{}, nil
	}

	var output LsblkOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		// Log raw output for debugging if JSON fails
		log.Printf("Failed to unmarshal lsblk output: %s", stdout)
		return nil, err
	}

	// Filter only items with type "disk"
	var systemDisks []Disk
	for _, block := range output.BlockDevices {
		if block.Type == "disk" {
			systemDisks = append(systemDisks, block)
		}
	}
	return systemDisks, nil
}

// --- Handlers ---

func handleGetDisks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	disks, err := getSystemDisks("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(disks)
}

func handleGetEmptyDisks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	allDisks, err := getSystemDisks("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var emptyDisks []Disk
	for _, disk := range allDisks {
		// Python logic: not disk.children and (not "nbd" in disk.name)
		if len(disk.Children) == 0 && !strings.Contains(disk.Name, "nbd") {
			emptyDisks = append(emptyDisks, disk)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(emptyDisks)
}

func handleGetOSDDisks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	baseDir := "/dev/disk/by-partuuid"
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		// If directory doesn't exist or other error, return empty list or error based on logic.
		// Python code returned None (null in JSON) if list was "0" (likely empty or dir missing).
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("null"))
			return
		}
		log.Printf("Error reading %s: %v", baseDir, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(entries) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("null"))
		return
	}

	var osdInfoList []VitastorParameters

	for _, entry := range entries {
		// vitastor-disk read-sb '/dev/disk/by-partuuid/{osd}'
		fullPath := filepath.Join(baseDir, entry.Name())
		// log.Printf("Reading SB for %s", fullPath) // Debug similar to python's print

		code, stdout, stderr := execShell("vitastor-disk", "read-sb", fullPath)
		if code != 0 {
			log.Printf("Error getting info for %s, likely not OSD: %s", entry.Name(), stderr)
			continue
		}

		var params VitastorParameters
		if err := json.Unmarshal([]byte(stdout), &params); err != nil {
			log.Printf("Failed to parse vitastor info for %s: %v", entry.Name(), err)
			continue
		}
		// log.Printf("OSD info: %s", stdout)
		osdInfoList = append(osdInfoList, params)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(osdInfoList)
}

func handlePrepareDisk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse body
	var device OSDPrepareParameters
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(body, &device); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Check if disk is already partitioned
	alignedDisks, err := getSystemDisks(device.Disk)
	if err != nil {
		http.Error(w, "Failed to check disk status", http.StatusInternalServerError)
		return
	}
	if len(alignedDisks) == 0 {
		http.Error(w, "Disk not found", http.StatusNotFound)
		return
	}
	if len(alignedDisks[0].Children) > 0 {
		http.Error(w, "That disk already partitioned and can't be prepared automatically. Please check it manually", http.StatusConflict)
		return
	}

	// Prepare arguments for vitastor-disk
	// vitastor-disk prepare {device.disk} [--osd_per_disk {num}]
	args := []string{"prepare", device.Disk}
	if device.OSDNum != nil && *device.OSDNum > 0 {
		args = append(args, "--osd_per_disk", fmt.Sprintf("%d", *device.OSDNum))
	}

	code, _, stderr := execShell("vitastor-disk", args...)
	if code != 0 {
		log.Printf("Error preparing disk: %s", stderr)
		http.Error(w, "Error during preparing: "+stderr, http.StatusInternalServerError)
		return
	}

	// Re-scan disk to get new partitions
	diskList, err := getSystemDisks(device.Disk)
	if err != nil || len(diskList) == 0 {
		log.Printf("Failed to rescan disk after prepare")
		http.Error(w, "Failed to rescan disk", http.StatusInternalServerError)
		return
	}

	var vitastorParams []VitastorParameters

	for _, d := range diskList[0].Children {
		// log.Println(d.Name)
		code, stdout, stderr := execShell("vitastor-disk", "read-sb", d.Name)
		if code != 0 {
			log.Printf("Something happen during gathering OSD info for %s: %s", d.Name, stderr)
			continue
		}

		var params VitastorParameters
		if err := json.Unmarshal([]byte(stdout), &params); err != nil {
			log.Printf("Failed to parse SB JSON for %s", d.Name)
			continue
		}
		vitastorParams = append(vitastorParams, params)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(vitastorParams)
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/disk", handleGetDisks)
	mux.HandleFunc("/disk/empty", handleGetEmptyDisks)
	mux.HandleFunc("/disk/osd", handleGetOSDDisks)
	mux.HandleFunc("/disk/prepare", handlePrepareDisk)

	// Simple logging middleware
	loggedMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		mux.ServeHTTP(w, r)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	log.Printf("Starting Vitastor Agent on port %s...", port)
	if err := http.ListenAndServe(":"+port, loggedMux); err != nil {
		log.Fatal(err)
	}
}