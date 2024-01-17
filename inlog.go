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
	"strconv"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/net"
	"gopkg.in/yaml.v2"
)

var configPath = "config.yaml"

type Details struct {
	UserUUID     string  `json:"user_uuid"`
	Name         string  `json:"name"`
	CPUNumber    int     `json:"cpu_n_cores"`
	CPUFrequency float64 `json:"cpu_frequency"`
	MemoryTotal  uint64  `json:"memory_total"`
	SOArch       string  `json:"so_arch"`
	SOName       string  `json:"so_name"`
	SOVersion    string  `json:"so_version"`
	MachineUUID  string  `json:"machine_uuid"`
}

type Payload struct {
	Details Details `json:"details"`
}

func getConfig() map[string]string {
	config := make(map[string]string)

	configPathFile := filepath.Dir(os.Args[0]) + "/" + configPath

	if _, err := os.Stat(configPathFile); os.IsNotExist(err) {
		// configpath does not exist
		return config
	}

	data, err := os.ReadFile(configPathFile)
	if err != nil {
		return config
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return config
	}

	return config
}

func setConfig(key string, value string) {
	config := getConfig()
	config[key] = value
	saveConfig(config)
}

func saveConfig(config map[string]string) error {

	configPathFile := filepath.Dir(os.Args[0]) + "/" + configPath

	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	err = os.WriteFile(configPathFile, data, 0644)
	if err != nil {
		return err
	}

	return nil
}

func getServiceExecPath() string {
	return getCurrentDirectory() + "/inlog run"
}

func getCurrentDirectory() string {
	if exePath, err := os.Executable(); err == nil {
		return filepath.Dir(exePath)
	}
	return ""
}

func runInlog() {
	config := getConfig()

	// verifique se existe user_uuid em config
	if _, ok := config["user_uuid"]; !ok {
		fmt.Println("You haven't configured your instance yet, please run the ./inlog configure user_uuid <user_id> command")
		os.Exit(1)
	}

	loopTime := 60
	if _, ok := config["loop_time"]; ok {
		loopTimeStr := config["loop_time"]
		loopTime, _ = strconv.Atoi(loopTimeStr)
	}

	instanceName := "default-instance"
	if _, ok := config["instance_name"]; ok {
		instanceName = config["instance_name"]
	}

	cpuInfo, _ := cpu.Info()

	cpuCount := 0
	cpuFreq := 0.0
	memInfo, _ := mem.VirtualMemory()
	soInfo, _ := host.Info()

	if len(cpuInfo) > 0 {
		cpuCount = len(cpuInfo)
		cpuFreq = cpuInfo[0].Mhz
	}

	payload := map[string]interface{}{
		"details": map[string]interface{}{
			"user_uuid":     config["user_uuid"],
			"name":          instanceName,
			"cpu_n_cores":   cpuCount,
			"cpu_frequency": cpuFreq,
			"memory_total":  memInfo.Total,
			"so_arch":       soInfo.Platform,
			"so_name":       soInfo.OS,
			"so_version":    soInfo.PlatformVersion,
		},
	}

	hasMachineUUID := false

	if _, ok := config["machine_uuid"]; ok {
		payload["machine_uuid"] = config["machine_uuid"]
		hasMachineUUID = true
	}

	jsonPayload, _ := json.Marshal(payload)

	url := "http://154.53.35.160:3800/v1/engine/machine/"

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		fmt.Println("Error creating HTTP request:", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending HTTP request:", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return
	}

	if !hasMachineUUID {
		var responseJSON map[string]interface{}
		err := json.Unmarshal(body, &responseJSON)
		if err != nil {
			fmt.Println("Error unmarshaling response body:", err)
			return
		}

		if data, ok := responseJSON["data"].(map[string]interface{}); ok {
			if machineUUID, ok := data["_id"].(string); ok {
				config["machine_uuid"] = machineUUID
				saveConfig(config)
			}
		}
	}

	println("Machine registered successfully!!!")

	for {
		logUnit()
		time.Sleep(time.Second * time.Duration(loopTime)) // adjust the time interval as needed
	}
}

func logUnit() {

	config := getConfig()

	url := "http://154.53.35.160:3800/v1/engine/log_monitor/"

	// Obtenção de informações de CPU
	cpuPercent, _ := cpuPercent()
	loadAvg, _ := load.Avg()

	// Informações de memória
	virtualMemory, _ := mem.VirtualMemory()

	// Informações de rede
	netCounters, _ := net.IOCounters(false)

	diskName := "sda"
	if _, ok := config["disk_name"]; ok {
		diskName = config["disk_name"]
	}

	diskPath := "/"
	if _, ok := config["disk_path"]; ok {
		diskPath = config["disk_path"]
	}

	diskIO, _ := disk.IOCounters(diskName)
	diskUsage, _ := disk.Usage(diskPath)

	payload := map[string]interface{}{
		"machine_uuid": config["machine_uuid"],
		"cpu": map[string]interface{}{
			"current_cpu": cpuPercent,
			"m1":          loadAvg.Load1,
			"m5":          loadAvg.Load5,
			"m15":         loadAvg.Load15,
		},
		"memory": map[string]interface{}{
			"available": virtualMemory.Available,
			"percent":   virtualMemory.UsedPercent,
			"used":      virtualMemory.Used,
			"free":      virtualMemory.Free,
			"active":    virtualMemory.Active,
			"inactive":  virtualMemory.Inactive,
			"buffers":   virtualMemory.Buffers,
			"cached":    virtualMemory.Cached,
			"shared":    virtualMemory.Shared,
			"slab":      virtualMemory.Slab,
		},
		"network": map[string]interface{}{
			"bytes_sent":   netCounters[0].BytesSent,
			"bytes_recv":   netCounters[0].BytesRecv,
			"packets_sent": netCounters[0].PacketsSent,
			"packets_recv": netCounters[0].PacketsRecv,
			"errin":        netCounters[0].Errin,
			"errout":       netCounters[0].Errout,
			"dropin":       netCounters[0].Dropin,
			"dropout":      netCounters[0].Dropout,
		},
		"disk_io": map[string]interface{}{
			"read_count":  diskIO[diskName].ReadCount,
			"write_count": diskIO[diskName].WriteCount,
			"read_bytes":  diskIO[diskName].ReadBytes,
			"write_bytes": diskIO[diskName].WriteBytes,
			"read_time":   diskIO[diskName].ReadTime,
			"write_time":  diskIO[diskName].WriteTime,
			"busy_time":   diskIO[diskName].IoTime,
		},
		"disk_usage": map[string]interface{}{
			"percent": diskUsage.UsedPercent,
			"total":   diskUsage.Total,
			"used":    diskUsage.Used,
			"free":    diskUsage.Free,
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("Erro ao codificar dados em JSON: %s", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("Erro ao criar requisição HTTP: %s", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Erro ao enviar requisição HTTP: %s", err)
	}
	defer resp.Body.Close()
}

func cpuPercent() (float64, error) {
	percent, err := cpu.Percent(time.Second, false)
	if err != nil {
		return 0, err
	}
	return percent[0], nil
}

func showHelp() {
	fmt.Println("The command was not found, the accepted commands are:")
	fmt.Println("./inlog configure user_uuid <user_uuid>")
	fmt.Println("./inlog run")
	fmt.Println("./inlog service")
}

func main() {
	args := os.Args[1:]

	if len(args) < 1 {
		showHelp()
		os.Exit(1)
	}

	if args[0] == "service" {
		execPath := getServiceExecPath()

		print(execPath)

		serviceContent := fmt.Sprintf(`[Unit]
			Description=Inlog Client

			[Service]
			ExecStartPre=/bin/bash -c 'cd "$(dirname "$0")"'
			ExecStart=%s
			Restart=no

			[Install]
			WantedBy=multi-user.target
		`, execPath)

		filePath := "/etc/systemd/system/inlog.service"

		err := os.WriteFile(filePath, []byte(serviceContent), 0644)
		if err != nil {
			fmt.Println("Error writing service file:", err)
			os.Exit(1)
		}

		cmd := exec.Command("sudo", "systemctl", "daemon-reload")
		err = cmd.Run()
		if err != nil {
			fmt.Println("Error running daemon-reload command:", err)
			os.Exit(1)
		}

		cmd = exec.Command("sudo", "systemctl", "enable", "inlog")
		err = cmd.Run()
		if err != nil {
			fmt.Println("Error enabling service:", err)
			os.Exit(1)
		}

		cmd = exec.Command("sudo", "systemctl", "restart", "inlog")
		err = cmd.Run()
		if err != nil {
			fmt.Println("Error starting service:", err)
			os.Exit(1)
		}

		fmt.Println("Service installed successfully")
		os.Exit(0)
	}

	if args[0] == "run" {
		runInlog()
		os.Exit(0)
	}

	if args[0] == "test" {

		getConfig()
		os.Exit(0)

		url := "https://api.telegram.org/bot6316859141:AAEjY-x-XR03PC9NFlQRmBTz860doktbOh0/sendMessage?chat_id=973974621&text=InLog RUNNING"

		resp, err := http.Get(url)
		if err != nil {
			fmt.Println("Erro ao fazer a requisição:", err)
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Println("Erro ao ler resposta:", err)
			return
		}

		fmt.Println(string(body))

		os.Exit(0)
	}

	if args[0] == "help" {
		showHelp()
		os.Exit(0)
	}

	if args[0] == "configure" {
		if len(args) < 3 {
			fmt.Println("You must specify the key and value to be set")
			os.Exit(1)
		}

		setConfig(args[1], args[2])
		os.Exit(0)
	}

	showHelp()
	os.Exit(1)
}
