// SPDX-License-Identifier: Apache-2.0
package main

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tturner/rangerdanger/services/devices/fielddevice"
	"github.com/tturner/rangerdanger/services/devices/httpapi"
	"github.com/tturner/rangerdanger/services/devices/modbus"
)

const defaultZones = "field=10.40.40.0/24,station=10.30.30.0/24,process=10.50.50.0/24,mgmt=10.99.99.0/24"

type config struct {
	protection    fielddevice.ProtectionSettings
	kind          fielddevice.Kind
	normal        fielddevice.Position
	deviceID      string
	modbusListen  string
	processListen string
	simListen     string
	zones         *fielddevice.ZoneMap
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	generation, err := randomGeneration()
	if err != nil {
		log.Fatalf("generate device generation: %v", err)
	}
	model, err := fielddevice.NewModel(fielddevice.Config{
		Protection: &cfg.protection, Kind: cfg.kind, RMUNormal: cfg.normal,
		DeviceID:   cfg.deviceID,
		Generation: generation,
		LocalContext: fielddevice.AuditContext{
			Peer: "protection", Protocol: "local-protection", Zone: cfg.zones.Name("field"),
		},
		ProcessContext: fielddevice.AuditContext{
			Peer: "measurement-validity", Protocol: "process", Zone: cfg.zones.Name("process"),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	modbusListener, err := net.Listen("tcp", cfg.modbusListen)
	if err != nil {
		log.Fatalf("listen for Modbus on %s: %v", cfg.modbusListen, err)
	}
	processListener, err := net.Listen("tcp", cfg.processListen)
	if err != nil {
		_ = modbusListener.Close()
		log.Fatalf("listen for process API on %s: %v", cfg.processListen, err)
	}
	simListener, err := net.Listen("tcp", cfg.simListen)
	if err != nil {
		_ = modbusListener.Close()
		_ = processListener.Close()
		log.Fatalf("listen for sim API on %s: %v", cfg.simListen, err)
	}

	go monitorMeasurementValidity(model)
	api := httpapi.New(model, cfg.zones)
	processServer := httpapi.NewHTTPServer(cfg.processListen, api.ProcessHandler())
	simServer := httpapi.NewHTTPServer(cfg.simListen, api.SimHandler())
	modbusServer := modbus.New(model, cfg.zones)
	errorsFromServers := make(chan error, 3)
	go func() { errorsFromServers <- modbusServer.Serve(modbusListener) }()
	go func() { errorsFromServers <- processServer.Serve(processListener) }()
	go func() { errorsFromServers <- simServer.Serve(simListener) }()

	log.Printf("field-device-sim %q generation=%d modbus=%s process=%s sim=%s",
		cfg.deviceID, generation, cfg.modbusListen, cfg.processListen, cfg.simListen)
	err = <-errorsFromServers
	_ = processServer.Close()
	_ = simServer.Close()
	_ = modbusListener.Close()
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		log.Fatal("a listener stopped unexpectedly")
	}
	log.Fatalf("a listener failed: %v", err)
}

func loadConfig() (config, error) {
	kind := fielddevice.Kind(os.Getenv("DEVICE_KIND"))
	if kind != fielddevice.Breaker && kind != fielddevice.RMU && kind != fielddevice.OLTC {
		return config{}, errors.New("DEVICE_KIND must be breaker, rmu or oltc")
	}
	normal := fielddevice.Position(os.Getenv("DEVICE_RMU_NORMAL"))
	if kind == fielddevice.RMU && normal != fielddevice.PositionClosed && normal != fielddevice.PositionOpen {
		return config{}, errors.New("DEVICE_RMU_NORMAL must be closed or open")
	}
	deviceID := os.Getenv("DEVICE_ID")
	if strings.TrimSpace(deviceID) == "" {
		return config{}, errors.New("DEVICE_ID must not be empty")
	}
	modbusListen, err := httpapi.ListenAddress(envOrDefault("DEVICE_MODBUS_LISTEN", "10.40.40.20:502"))
	if err != nil {
		return config{}, err
	}
	processListen, err := httpapi.ListenAddress(envOrDefault("DEVICE_PROCESS_LISTEN", "10.50.50.30:8080"))
	if err != nil {
		return config{}, err
	}
	simListen, err := httpapi.ListenAddress(envOrDefault("DEVICE_SIM_LISTEN", "10.99.99.22:8080"))
	if err != nil {
		return config{}, err
	}
	zones, err := fielddevice.ParseZones(envOrDefault("DEVICE_ZONES", defaultZones))
	if err != nil {
		return config{}, err
	}
	protection := fielddevice.DefaultProtection(kind)
	if raw, ok := os.LookupEnv("DEVICE_PICKUP_A"); ok {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return config{}, fmt.Errorf("DEVICE_PICKUP_A: %w", err)
		}
		protection.PickupA = value
	}
	for name, dest := range map[string]*int64{"DEVICE_DELAY_MS": &protection.DelayMS, "DEVICE_MAX_GAP_MS": &protection.MaxGapMS} {
		if raw, ok := os.LookupEnv(name); ok {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return config{}, fmt.Errorf("%s: %w", name, err)
			}
			*dest = value
		}
	}
	if raw, ok := os.LookupEnv("DEVICE_INSTANTANEOUS_A"); ok {
		if raw == "null" {
			protection.InstantaneousA = nil
		} else {
			value, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return config{}, err
			}
			protection.InstantaneousA = &value
		}
	}
	return config{
		protection: protection,
		kind:       kind, normal: normal,
		deviceID:      deviceID,
		modbusListen:  modbusListen,
		processListen: processListen,
		simListen:     simListen,
		zones:         zones,
	}, nil
}

func envOrDefault(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}

func randomGeneration() (uint32, error) {
	var value [4]byte
	if _, err := rand.Read(value[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(value[:]), nil
}

func monitorMeasurementValidity(model *fielddevice.Model) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		model.Snapshot()
	}
}
