package main

import (
	"encoding/json"
	"fmt"
	"machine"
	"time"
	"tinygo.org/x/drivers/dht"
)

type SensorData struct {
	Temperature  float64 `json:"temperature"`
	Humidity     float64 `json:"humidity"`
	SoilMoisture float64 `json:"soil_moisture"`
}

func main() {
	fmt.Println("🚀 Smart Farm Sensors: Starting up...")

	// ✅ Initialize Sensors
	time.Sleep(2 * time.Second)
	dhtSensor := dht.New(machine.GP2, dht.DHT22)
	machine.InitADC()
	adc := machine.ADC{Pin: machine.GP27}
	adc.Configure(machine.ADCConfig{})

	fmt.Println("✅ Sensors Initialized!")

	for {
		// ✅ Read Soil Moisture
		soilRaw := adc.Get()
		soilVoltage := float32(soilRaw) * 3.3 / 65535.0
		soilMoisture := 100 - ((soilVoltage / 3.3) * 100)

		// ✅ Read Temperature & Humidity
		temp, hum, err := dhtSensor.Measurements()
		if err != nil {
			fmt.Println("❌ Error reading DHT22:", err)
			continue
		}

		// ✅ Format Data as JSON
		data := SensorData{
			Temperature:  float64(temp) / 10.0,
			Humidity:     float64(hum) / 10.0,
			SoilMoisture: float64(soilMoisture),
		}

		jsonData, err := json.Marshal(data)
		if err != nil {
			fmt.Println("❌ JSON Encoding Error:", err)
			continue
		}

		// ✅ Print Data to Serial (MacBook `server.go` will read this)
		fmt.Println(string(jsonData))

		time.Sleep(5 * time.Second)
	}
}