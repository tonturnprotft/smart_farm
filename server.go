package main

import (
    "bytes"
    "bufio"
    "database/sql"
    "encoding/json"
    "fmt"
    "image/color"
    "io/ioutil"
    "log"
    "net/http"
    "time"

    mqtt "github.com/eclipse/paho.mqtt.golang"
    "github.com/gorilla/mux"
    _ "github.com/lib/pq"
    "github.com/tarm/serial"

    // Fyne
    "fyne.io/fyne/v2"
    "fyne.io/fyne/v2/app"
    "fyne.io/fyne/v2/canvas"
    "fyne.io/fyne/v2/container"
    "fyne.io/fyne/v2/layout"
    "fyne.io/fyne/v2/widget"
)

const dbConn = "user=postgres password=4847 dbname=farm_db sslmode=disable"

var (
    db         *sql.DB
    serialPort *serial.Port
    mqttClient mqtt.Client
)

// SensorData …
type SensorData struct {
    Temperature  float64 `json:"temperature"`
    AirHumidity  float64 `json:"humidity"`
    SoilMoisture float64 `json:"soil_moisture"`
    Brightness   float64 `json:"brightness"`
}

// -------------------------------------------------------------------
// 1) MQTT Handler
// -------------------------------------------------------------------
func mqttMessageHandler(client mqtt.Client, msg mqtt.Message) {
    var data SensorData
    fmt.Println("📡 Incoming MQTT Data:", string(msg.Payload()))
    err := json.Unmarshal(msg.Payload(), &data)
    if err != nil {
        fmt.Println("❌ Error decoding MQTT:", err)
        return
    }
    // Insert to DB etc. ...
}

// -------------------------------------------------------------------
// 2) Publish to MQTT
// -------------------------------------------------------------------
func publishToMQTT(data SensorData) {
    if mqttClient == nil {
        fmt.Println("❌ MQTT Client not initialized")
        return
    }
    b, _ := json.Marshal(data)
    token := mqttClient.Publish("smartfarm/sensors", 0, false, b)
    token.Wait()
    if token.Error() != nil {
        fmt.Println("❌ MQTT Publish Error:", token.Error())
    } else {
        fmt.Println("✅ Sent to MQTT:", string(b))
    }
}

// -------------------------------------------------------------------
// 3) Read from Serial & Store in DB
// -------------------------------------------------------------------
func readSerial() {
    reader := bufio.NewReader(serialPort)
    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            fmt.Println("❌ Error reading from serial:", err)
            time.Sleep(time.Second)
            continue
        }
        var data SensorData
        if err := json.Unmarshal([]byte(line), &data); err != nil {
            fmt.Println("❌ JSON Parsing Error:", err, "| line:", line)
            continue
        }
        // Insert DB ...
        _, err = db.Exec(`INSERT INTO sensor (date, time, temp, air_humidity, soil_humidity, brightness)
            VALUES (CURRENT_DATE, CURRENT_TIME, $1, $2, $3, 50)`,
            data.Temperature, data.AirHumidity, data.SoilMoisture)
        if err != nil {
            fmt.Println("❌ DB Error:", err)
        } else {
            fmt.Println("✅ Sensor Data Stored:", data)
        }

        // Publish to MQTT
        publishToMQTT(data)
    }
}

// -------------------------------------------------------------------
// 4) fetchSensorData (Web API)
// -------------------------------------------------------------------
func fetchSensorData(w http.ResponseWriter, r *http.Request) {
    row := db.QueryRow(`SELECT temp, air_humidity, soil_humidity, brightness
        FROM sensor ORDER BY id DESC LIMIT 1`)

    var data SensorData
    err := row.Scan(&data.Temperature, &data.AirHumidity, &data.SoilMoisture, &data.Brightness)
    if err != nil {
        http.Error(w, "No data or DB Error", http.StatusNotFound)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(data)
}

// -------------------------------------------------------------------
// 5) serveHTML
// -------------------------------------------------------------------
func serveHTML(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "index.html")
}

// -------------------------------------------------------------------
// 6) controlPump (API): รับ JSON {"command":"on"|"off"} -> ส่งลง Serial -> รอ ACK
// -------------------------------------------------------------------
func controlPump(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Command string `json:"command"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }
    if req.Command != "on" && req.Command != "off" {
        http.Error(w, "Use 'on' or 'off'", http.StatusBadRequest)
        return
    }

    _, err := serialPort.Write([]byte(req.Command + "\n"))
    if err != nil {
        http.Error(w, "Failed to write serial", http.StatusInternalServerError)
        return
    }

    // รอ ACK
    serialPort.Flush()
    ackReader := bufio.NewReader(serialPort)
    ackLine, err := ackReader.ReadString('\n')
    if err != nil {
        http.Error(w, "No ACK or read error", http.StatusGatewayTimeout)
        return
    }

    // ส่งกลับ
    resp := map[string]string{"ack": ackLine}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

// -------------------------------------------------------------------
// 7) GUI Fyne
// -------------------------------------------------------------------
func createGUI() {
    myApp := app.New()
    myWindow := myApp.NewWindow("Smart Farm Control Panel")
    myWindow.Resize(fyne.NewSize(600, 600))

    // Title
    title := canvas.NewText("🌱 Smart Farm Control Panel", color.White)
    title.TextSize = 20
    title.TextStyle.Bold = true

    // Pump label
    pumpStatus := canvas.NewText("Stop watering the plants...", color.White)
    pumpStatus.TextSize = 14
    pumpStatus.TextStyle.Bold = true

    var pumpOnButton, pumpOffButton *widget.Button

    pumpOnButton = widget.NewButton("Turn On", func() {
        // ส่ง HTTP ไป /control-pump { "command":"on" }
        ack, err := sendPumpCommand("on")
        if err != nil {
            pumpStatus.Text = fmt.Sprintf("Error: %v", err)
        } else {
            pumpStatus.Text = ack
        }
        pumpOnButton.Disable()
        pumpOffButton.Enable()
        myWindow.Canvas().Refresh(pumpStatus)
    })

    pumpOffButton = widget.NewButton("Turn Off", func() {
        ack, err := sendPumpCommand("off")
        if err != nil {
            pumpStatus.Text = fmt.Sprintf("Error: %v", err)
        } else {
            pumpStatus.Text = ack
        }
        pumpOffButton.Disable()
        pumpOnButton.Enable()
        myWindow.Canvas().Refresh(pumpStatus)
    })

    pumpOffButton.Disable() // เริ่มต้นปุ่ม OFF เป็น disable

    content := container.NewVBox(
        container.NewCenter(title),
        container.NewCenter(pumpStatus),
        container.NewHBox(
            layout.NewSpacer(),
            pumpOnButton,
            layout.NewSpacer(),
            pumpOffButton,
            layout.NewSpacer(),
        ),
    )

    myWindow.SetContent(content)
    myWindow.ShowAndRun()
}

// ฟังก์ชันส่ง command ผ่าน HTTP ไป /control-pump
func sendPumpCommand(cmd string) (string, error) {
    data := fmt.Sprintf(`{"command":"%s"}`, cmd)
    resp, err := http.Post("http://localhost:8080/control-pump",
        "application/json",
        bytes.NewBuffer([]byte(data)))
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return "", err
    }

    var result map[string]string
    if err := json.Unmarshal(body, &result); err != nil {
        return "", err
    }

    return result["ack"], nil
}

// -------------------------------------------------------------------
// 8) main
// -------------------------------------------------------------------
func main() {
    // 1) Connect DB
    var err error
    db, err = sql.Open("postgres", dbConn)
    if err != nil {
        log.Fatal("DB Error:", err)
    }
    defer db.Close()

    // 2) เปิด Serial
    c := &serial.Config{Name: "/dev/tty.usbmodem1301", Baud: 115200, ReadTimeout: time.Second * 2}
    serialPort, err = serial.OpenPort(c)
    if err != nil {
        log.Fatal("❌ Error opening serial port:", err)
    }
    fmt.Println("✅ Serial port opened!")
    opts := mqtt.NewClientOptions()
    opts.AddBroker("tcp://localhost:1883") // หรือ Broker อื่น
    mqttClient = mqtt.NewClient(opts)

    token := mqttClient.Connect()
    token.Wait()
    if token.Error() != nil {
        log.Fatal("❌ MQTT Connection Error:", token.Error())
    }
    fmt.Println("✅ MQTT Client connected")

    // 3) อ่าน Sensor
    go readSerial()

    // 4) Web Server
    router := mux.NewRouter()
    router.HandleFunc("/", serveHTML)
    router.HandleFunc("/sensor-data", fetchSensorData).Methods("GET")
    router.HandleFunc("/control-pump", controlPump).Methods("POST")
    fmt.Println("✅ Server running on http://localhost:8080")
    go func() {
        log.Fatal(http.ListenAndServe(":8080", router))
    }()

    // 5) รัน GUI
    createGUI()

    serialPort.Close()
}