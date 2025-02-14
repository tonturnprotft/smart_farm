package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	//"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

var serverURL = "http://localhost:8080/update"

type SensorData struct {
	Brightness int `json:"brightness"`
	WaterLevel int `json:"water_level"`
}

func sendData(brightness, waterLevel int) {
	data := SensorData{Brightness: brightness, WaterLevel: waterLevel}
	jsonData, _ := json.Marshal(data)

	resp, err := http.Post(serverURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("Error sending data:", err)
		return
	}
	defer resp.Body.Close()

	fmt.Println("Data sent successfully:", resp.Status)
}

func main() {
	a := app.New()
	w := a.NewWindow("Smart Farm Control")
	w.Resize(fyne.NewSize(400, 300))

	brightnessSlider := widget.NewSlider(0, 100)
	waterLevelSlider := widget.NewSlider(0, 100)

	brightnessSlider.OnChanged = func(value float64) {
		sendData(int(value), int(waterLevelSlider.Value))
	}
	waterLevelSlider.OnChanged = func(value float64) {
		sendData(int(brightnessSlider.Value), int(value))
	}

	w.SetContent(container.NewVBox(
		widget.NewLabel("Brightness Control"),
		brightnessSlider,
		widget.NewLabel("Water Level Control"),
		waterLevelSlider,
	))

	w.ShowAndRun()
}