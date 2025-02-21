package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	//"os"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/tarm/serial"
)

// ✅ PostgreSQL Connection
const dbConn = "user=postgres password=4847 dbname=farm_db sslmode=disable"

var db *sql.DB

// ✅ Sensor Data Structure
type SensorData struct {
	Temperature  float64 `json:"temperature"`
	AirHumidity  float64 `json:"humidity"`
	SoilMoisture float64 `json:"soil_moisture"`
	Brightness   float64 `json:"brightness"`
}
// ✅ MQTT Message Handler - Store in Database
func mqttMessageHandler(client mqtt.Client, msg mqtt.Message) {
	var data SensorData

	fmt.Println("📡 Incoming MQTT Data:", string(msg.Payload()))

	err := json.Unmarshal(msg.Payload(), &data)
	if err != nil {
		fmt.Println("❌ Error decoding MQTT message:", err)
		return
	}

	fmt.Println("📡 Parsed Sensor Data:", data)

	// ✅ Fix: Insert `Temperature` into the `temp` column
	_, err = db.Exec(`INSERT INTO sensor (date, time, temp, air_humidity, soil_humidity, brightness)
		VALUES (CURRENT_DATE, CURRENT_TIME, $1, $2, $3, 50)`, data.Temperature, data.AirHumidity, data.SoilMoisture)

	if err != nil {
		fmt.Println("❌ Error storing data in DB:", err)
	} else {
		fmt.Println("✅ Data stored successfully in PostgreSQL")
	}
}
// ✅ Send Data to MQTT Broker
func publishToMQTT(data SensorData) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker("tcp://localhost:1883")

	client := mqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()

	if token.Error() != nil {
		fmt.Println("❌ MQTT Connection Failed:", token.Error())
		return
	}

	jsonData, _ := json.Marshal(data)
	token = client.Publish("smartfarm/sensors", 0, false, jsonData)
	token.Wait()
	if token.Error() != nil {
		fmt.Println("❌ Error sending MQTT message:", token.Error())
	} else {
		fmt.Println("✅ Data Sent to MQTT:", string(jsonData))
	}
}

// ✅ Read from Serial & Store in DB
func readSerial() {
	config := &serial.Config{Name: "/dev/tty.usbmodem11201", Baud: 115200} // 🔹 Adjust for Mac
	port, err := serial.OpenPort(config)
	if err != nil {
		log.Fatal("❌ Error opening serial port:", err)
	}
	defer port.Close()

	reader := bufio.NewReader(port)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("❌ Error reading from serial:", err)
			continue
		}

		var data SensorData
		err = json.Unmarshal([]byte(line), &data)
		if err != nil {
			fmt.Println("❌ JSON Parsing Error:", err)
			continue
		}

		// ✅ Fix: Insert `Temperature` into the `temp` column
		_, err = db.Exec(`INSERT INTO sensor (date, time, temp, air_humidity, soil_humidity, brightness)
			VALUES (CURRENT_DATE, CURRENT_TIME, $1, $2, $3, 50)`, data.Temperature, data.AirHumidity, data.SoilMoisture)

		if err != nil {
			fmt.Println("❌ Error storing data in DB:", err)
		} else {
			fmt.Println("✅ Sensor Data Stored:", data)
		}

		// ✅ Send to MQTT
		publishToMQTT(data)
	}
}

// ✅ Start Web API
func fetchSensorData(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT temp, air_humidity, soil_humidity, brightness FROM sensor ORDER BY id DESC LIMIT 1`)
	if err != nil {
		http.Error(w, "❌ Failed to retrieve data", http.StatusInternalServerError)
		fmt.Println("❌ SQL Query Error:", err)
		return
	}
	defer rows.Close()

	var data SensorData
	if rows.Next() {
		err := rows.Scan(&data.Temperature, &data.AirHumidity, &data.SoilMoisture, &data.Brightness)
		if err != nil {
			http.Error(w, "❌ Error scanning data", http.StatusInternalServerError)
			fmt.Println("❌ Error scanning row:", err)
			return
		}
	} else {
		http.Error(w, "❌ No data found", http.StatusNotFound)
		fmt.Println("❌ No data found in database")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
	fmt.Println("✅ Sent Sensor Data:", data)
}

// ✅ Serve HTML Dashboard
func serveHTML(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func main() {
	var err error
	db, err = sql.Open("postgres", dbConn)
	if err != nil {
		log.Fatal("❌ Database connection error:", err)
	}
	defer db.Close()

	// ✅ Start Serial Reader
	go readSerial()

	// ✅ Start Web Server
	router := mux.NewRouter()
	router.HandleFunc("/", serveHTML)               // ✅ Serve HTML
	router.HandleFunc("/sensor-data", fetchSensorData).Methods("GET") // ✅ Provide Data for Dashboard

	fmt.Println("✅ Server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}