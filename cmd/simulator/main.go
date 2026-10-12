package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/alulema/telemetry-ingest-iot/internal/simulator"
)

func main() {
	var config simulator.Config
	var includeStates bool
	flag.IntVar(&config.Devices, "devices", 1000, "number of simulated devices")
	flag.IntVar(&config.EventsPerDevice, "events-per-device", 60, "events generated per device")
	flag.Int64Var(&config.Seed, "seed", 20261007, "fixed random seed")
	flag.IntVar(&config.BufferCapacity, "buffer-capacity", 256, "per-device offline ring buffer capacity")
	flag.Float64Var(&config.DisconnectProbability, "disconnect-probability", 0.02, "per-event chance of transitioning online to offline")
	flag.Float64Var(&config.ReconnectProbability, "reconnect-probability", 0.25, "per-event chance of transitioning offline to online")
	flag.Float64Var(&config.DuplicateProbability, "duplicate-probability", 0.03, "retry duplicate probability")
	flag.IntVar(&config.ReorderWindow, "reorder-window", 16, "maximum shuffled batch size")
	flag.DurationVar(&config.Interval, "interval", time.Minute, "time between generated events")
	flag.BoolVar(&includeStates, "include-states", false, "include each device's final state in output")
	flag.Parse()

	report, err := simulator.Run(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if !includeStates {
		report.States = nil
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
