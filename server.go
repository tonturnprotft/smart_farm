package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	//"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

const dbConn = "user=postgres password=4847 dbname=farm_db sslmode=disable"

var db *sql.DB

type SensorData struct {
	ID           int     `json:"id"`
	Date         string  `json:"date"`
	Time         string  `json:"time"`
	AirHumidity  float64 `json:"air_humidity"`
	SoilHumidity float64 `json:"soil_humidity"`
	Brightness   int     `json:"brightness"`
}

// ✅ API to Fetch Sensor Data
func fetchSensorData(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, date, time, air_humidity, soil_humidity, brightness FROM sensor ORDER BY id DESC LIMIT 10`)
	if err != nil {
		http.Error(w, "Failed to retrieve data", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var sensorData []SensorData
	for rows.Next() {
		var data SensorData
		err := rows.Scan(&data.ID, &data.Date, &data.Time, &data.AirHumidity, &data.SoilHumidity, &data.Brightness)
		if err != nil {
			http.Error(w, "Error scanning data", http.StatusInternalServerError)
			return
		}
		sensorData = append(sensorData, data)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sensorData)
}

// ✅ API to Update Brightness
func setBrightness(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Only POST method allowed", http.StatusMethodNotAllowed)
		return
	}

	var requestData struct {
		Brightness int `json:"brightness"`
	}
	err := json.NewDecoder(r.Body).Decode(&requestData)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate range
	if requestData.Brightness < 0 || requestData.Brightness > 100 {
		http.Error(w, "Brightness must be between 0 and 100", http.StatusBadRequest)
		return
	}

	// Update database
	_, err = db.Exec(`UPDATE sensor SET brightness = $1 WHERE id = (SELECT MAX(id) FROM sensor)`, requestData.Brightness)
	if err != nil {
		http.Error(w, "Failed to update brightness", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Brightness updated to %d%%", requestData.Brightness)
}

// ✅ Serve HTML Dashboard
func serveHTML(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

// ✅ GUI Application for Brightness Control
func createGUI() {
	a := app.New()
	w := a.NewWindow("Smart Farm GUI")

	// Brightness slider
	slider := widget.NewSlider(0, 100)
	slider.SetValue(50)
	slider.OnChanged = func(value float64) {
		fmt.Printf("Brightness set to: %d%%\n", int(value))

		// Send request to update brightness
		go func() {
			_, err := http.Post(fmt.Sprintf("http://localhost:8080/set-brightness?value=%d", int(value)), "application/json", nil)
			if err != nil {
				log.Println("Failed to update brightness:", err)
			}
		}()
	}

	w.SetContent(container.NewVBox(
		widget.NewLabel("Adjust Brightness:"),
		slider,
	))

	w.Resize(fyne.NewSize(300, 200))
	w.ShowAndRun()
}

func main() {
	var err error
	db, err = sql.Open("postgres", dbConn)
	if err != nil {
		log.Fatal("Database connection error:", err)
	}
	defer db.Close()

	router := mux.NewRouter()
	router.HandleFunc("/", serveHTML) // ✅ Serve HTML
	router.HandleFunc("/sensor-data", fetchSensorData).Methods("GET") // ✅ Fetch Data
	router.HandleFunc("/set-brightness", setBrightness).Methods("POST") // ✅ Set Brightness

	go func() {
		fmt.Println("Server running on http://localhost:8080")
		log.Fatal(http.ListenAndServe(":8080", router))
	}()

	// Run GUI
	createGUI()
}