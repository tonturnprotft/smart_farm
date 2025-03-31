package main

import (
    "bufio"
    "bytes"
    "database/sql"
    "encoding/json"
    "fmt"
    "image/color"
    "io"
    "log"
    //"math"
    "net/http"
    "strings"
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

// --------------------------------------
// GLOBAL
// --------------------------------------
var (
    db         *sql.DB
    serialPort *serial.Port
    mqttClient mqtt.Client

    // Fyne
    myApp    fyne.App
    myWindow fyne.Window

    // Sliders + Progress
    slider1, slider2, slider3             *widget.Slider
    progressBar1, progressBar2, progressBar3 *widget.ProgressBar

    pumpOnButton, pumpOffButton *widget.Button
    pumpStatus                  *canvas.Text
)

// --------------------------------------
// SERVER & SERIAL
// --------------------------------------
type SensorData struct {
    Temperature  float64 `json:"temperature"`
    AirHumidity  float64 `json:"humidity"`
    SoilMoisture float64 `json:"soil_moisture"`
}

func readSerial() {
    reader := bufio.NewReader(serialPort)
    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            time.Sleep(time.Second)
            continue
        }
        line = strings.TrimSpace(line)
        if !strings.HasPrefix(line, "{") {
            fmt.Println("Non-JSON line:", line)
            continue
        }
        var data SensorData
        if err := json.Unmarshal([]byte(line), &data); err != nil {
            fmt.Println("❌ JSON Parsing Error:", err, "| line:", line)
            continue
        }
        // Insert DB
        _, err = db.Exec(`
            INSERT INTO sensor (date, time, temp, air_humidity, soil_humidity)
            VALUES (CURRENT_DATE, CURRENT_TIME, $1, $2, $3)`,
            data.Temperature, data.AirHumidity, data.SoilMoisture)
        if err != nil {
            fmt.Println("❌ DB Error:", err)
        } else {
            fmt.Printf("✅ Sensor Data: T=%.1f, H=%.1f, S=%.1f\n",
                data.Temperature, data.AirHumidity, data.SoilMoisture)
        }
    }
}

func serveHTML(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "index.html")
}

func fetchSensorData(w http.ResponseWriter, r *http.Request) {
    row := db.QueryRow(`SELECT temp, air_humidity, soil_humidity 
                        FROM sensor ORDER BY id DESC LIMIT 1`)
    var temp, hum, soil float64
    if err := row.Scan(&temp, &hum, &soil); err != nil {
        http.Error(w, "No data or DB Error", http.StatusNotFound)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    out := map[string]float64{
        "temperature":   temp,
        "humidity":      hum,
        "soil_moisture": soil,
    }
    json.NewEncoder(w).Encode(out)
}

// Pump
func controlPump(w http.ResponseWriter, r *http.Request) {
    var req struct{ Command string `json:"command"` }
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
    serialPort.Flush()
    ackR := bufio.NewReader(serialPort)
    ackLine, err := ackR.ReadString('\n')
    if err != nil {
        http.Error(w, "No ACK or read error", http.StatusGatewayTimeout)
        return
    }
    resp := map[string]string{"ack": strings.TrimSpace(ackLine)}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

// Light merged or single => but we keep multiple
// If you want multiple separate: /control-light13,14,15
func controlLight13(w http.ResponseWriter, r *http.Request) {
    var req struct{ Brightness int `json:"brightness"` }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }
    if req.Brightness < 0 || req.Brightness > 100 {
        http.Error(w, "Brightness must be 0..100", http.StatusBadRequest)
        return
    }
    cmd := fmt.Sprintf("light13:%d\n", req.Brightness)
    _, err := serialPort.Write([]byte(cmd))
    if err != nil {
        http.Error(w, "Failed to write serial", http.StatusInternalServerError)
        return
    }
    serialPort.Flush()
    ackR := bufio.NewReader(serialPort)
    ackLine, err := ackR.ReadString('\n')
    if err != nil {
        http.Error(w, "No Ack or read error", http.StatusGatewayTimeout)
        return
    }
    resp := map[string]string{"ack": strings.TrimSpace(ackLine)}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func controlLight14(w http.ResponseWriter, r *http.Request) {
    var req struct{ Brightness int `json:"brightness"` }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }
    if req.Brightness < 0 || req.Brightness > 100 {
        http.Error(w, "Brightness must be 0..100", http.StatusBadRequest)
        return
    }
    cmd := fmt.Sprintf("light14:%d\n", req.Brightness)
    _, err := serialPort.Write([]byte(cmd))
    if err != nil {
        http.Error(w, "Failed to write serial", http.StatusInternalServerError)
        return
    }
    serialPort.Flush()
    ackR := bufio.NewReader(serialPort)
    ackLine, err := ackR.ReadString('\n')
    if err != nil {
        http.Error(w, "No Ack or read error", http.StatusGatewayTimeout)
        return
    }
    resp := map[string]string{"ack": strings.TrimSpace(ackLine)}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func controlLight15(w http.ResponseWriter, r *http.Request) {
    var req struct{ Brightness int `json:"brightness"` }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }
    if req.Brightness < 0 || req.Brightness > 100 {
        http.Error(w, "Brightness must be 0..100", http.StatusBadRequest)
        return
    }
    cmd := fmt.Sprintf("light15:%d\n", req.Brightness)
    _, err := serialPort.Write([]byte(cmd))
    if err != nil {
        http.Error(w, "Failed to write serial", http.StatusInternalServerError)
        return
    }
    serialPort.Flush()
    ackR := bufio.NewReader(serialPort)
    ackLine, err := ackR.ReadString('\n')
    if err != nil {
        http.Error(w, "No Ack or read error", http.StatusGatewayTimeout)
        return
    }
    resp := map[string]string{"ack": strings.TrimSpace(ackLine)}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

// ---------------------------------------
// SEND function (GUI call)
func sendLight13Brightness(value int) {
    data := fmt.Sprintf(`{"brightness":%d}`, value)
    resp, err := http.Post("http://localhost:8080/control-light13",
        "application/json",
        bytes.NewBuffer([]byte(data)))
    if err != nil {
        fmt.Println("Error sending brightness 13:", err)
        return
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    fmt.Printf("Light13 Ack => %s\n", string(body))
}

func sendLight14Brightness(value int) {
    data := fmt.Sprintf(`{"brightness":%d}`, value)
    resp, err := http.Post("http://localhost:8080/control-light14",
        "application/json",
        bytes.NewBuffer([]byte(data)))
    if err != nil {
        fmt.Println("Error sending brightness 14:", err)
        return
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    fmt.Printf("Light14 Ack => %s\n", string(body))
}

func sendLight15Brightness(value int) {
    data := fmt.Sprintf(`{"brightness":%d}`, value)
    resp, err := http.Post("http://localhost:8080/control-light15",
        "application/json",
        bytes.NewBuffer([]byte(data)))
    if err != nil {
        fmt.Println("Error sending brightness 15:", err)
        return
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    fmt.Printf("Light15 Ack => %s\n", string(body))
}

func sendPumpCommand(cmd string) {
    data := fmt.Sprintf(`{"command":"%s"}`, cmd)
    resp, err := http.Post("http://localhost:8080/control-pump",
        "application/json",
        bytes.NewBuffer([]byte(data)))
    if err != nil {
        fmt.Println("Error pump command:", err)
        return
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    fmt.Printf("Pump Ack => %s\n", string(body))
}

// ---------------------------------------
// GUI
// ---------------------------------------
func createGUI() {
    myApp = app.New()
    myWindow = myApp.NewWindow("Smart Farm Control Panel")

    bgRect := canvas.NewRectangle(color.NRGBA{R: 33, G: 33, B: 33, A: 255})

    messageBackground := canvas.NewRectangle(color.NRGBA{R: 230, G: 230, B: 230, A: 255})
    messageBackground.SetMinSize(fyne.NewSize(600, 35))
    title := canvas.NewText("🌱 Smart Farm Control Panel", color.Black)
    title.TextSize = 20
    title.TextStyle = fyne.TextStyle{Bold: true}
    titlebox := container.NewMax(messageBackground, container.NewCenter(title))

    // Outer1
    brightness1Label := canvas.NewText("💡 Brightness Outer 1", color.White)
    brightness1Label.TextSize = 16
    brightness1Label.TextStyle = fyne.TextStyle{Bold: true}
    slider1 = widget.NewSlider(0, 100)
    progressBar1 = widget.NewProgressBar()
    slider1.OnChanged = func(v float64) {
        fmt.Printf("Brightness Outer1 => %d%%\n", int(v))
        progressBar1.SetValue(v / 100)
        // call /control-light13
        sendLight13Brightness(int(v))
    }

    // Outer2
    brightness2Label := canvas.NewText("💡 Brightness Outer 2", color.White)
    brightness2Label.TextSize = 16
    brightness2Label.TextStyle = fyne.TextStyle{Bold: true}
    slider2 = widget.NewSlider(0, 100)
    progressBar2 = widget.NewProgressBar()
    slider2.OnChanged = func(v float64) {
        fmt.Printf("Brightness Outer2 => %d%%\n", int(v))
        progressBar2.SetValue(v / 100)
        // call /control-light14
        sendLight14Brightness(int(v))
    }

    // Inner
    brightness3Label := canvas.NewText("💡 Brightness Inner", color.White)
    brightness3Label.TextSize = 16
    brightness3Label.TextStyle = fyne.TextStyle{Bold: true}
    slider3 = widget.NewSlider(0, 100)
    progressBar3 = widget.NewProgressBar()
    slider3.OnChanged = func(v float64) {
        fmt.Printf("Brightness Inner => %d%%\n", int(v))
        progressBar3.SetValue(v / 100)
        // call /control-light15
        sendLight15Brightness(int(v))
    }

    // Pump
    waterPumpLabel := canvas.NewText("💦 Water Pump", color.White)
    waterPumpLabel.TextSize = 16
    waterPumpLabel.TextStyle = fyne.TextStyle{Bold: true}
    pumpStatus = canvas.NewText("Stop watering the plants...", color.White)
    pumpStatus.TextSize = 14
    pumpStatus.TextStyle = fyne.TextStyle{Bold: true}

    pumpOnButton = widget.NewButton("Turn On", func() {
        fmt.Println("Pump ON (GUI)")
        pumpStatus.Text = "Watering the plants..."
        pumpOnButton.Disable()
        pumpOffButton.Enable()
        pumpOffButton.SetText("Turn Off")
        myWindow.Canvas().Refresh(pumpStatus)

        // call /control-pump => {command:on}
        sendPumpCommand("on")
    })
    pumpOffButton = widget.NewButton("Turn Off", func() {
        fmt.Println("Pump OFF (GUI)")
        pumpStatus.Text = "Stop watering the plants..."
        pumpOffButton.Disable()
        pumpOnButton.Enable()
        pumpOnButton.SetText("Turn On")
        myWindow.Canvas().Refresh(pumpStatus)
        sendPumpCommand("off")
    })
    pumpOffButton.Disable()

    // Layout
    content := container.NewVBox(
        container.NewCenter(titlebox),

        container.NewCenter(brightness1Label),
        slider1, progressBar1,

        container.NewCenter(brightness2Label),
        slider2, progressBar2,

        container.NewCenter(brightness3Label),
        slider3, progressBar3,

        container.NewCenter(waterPumpLabel),
        container.NewCenter(pumpStatus),
        container.NewHBox(
            layout.NewSpacer(),
            pumpOnButton,
            layout.NewSpacer(),
            pumpOffButton,
            layout.NewSpacer(),
        ),
    )

    myWindow.SetContent(container.NewMax(bgRect, content))
    myWindow.Resize(fyne.NewSize(600, 520))
}

func main() {
    // 1) Connect DB
    var err error
    db, err = sql.Open("postgres", dbConn)
    if err != nil {
        log.Fatal("DB Error:", err)
    }
    defer db.Close()

    // 2) Open Serial
    c := &serial.Config{Name: "/dev/tty.usbmodem1301", Baud: 115200, ReadTimeout: 2 * time.Second}
    serialPort, err = serial.OpenPort(c)
    if err != nil {
        log.Fatal("Error opening serial:", err)
    }
    fmt.Println("✅ Serial opened")

    // 3) MQTT (if needed)...

    // 4) Read Serial in background
    go readSerial()

    // 5) Setup router + run server
    router := mux.NewRouter()
    router.HandleFunc("/", serveHTML)
    router.HandleFunc("/sensor-data", fetchSensorData).Methods("GET")
    router.HandleFunc("/control-pump", controlPump).Methods("POST")
    router.HandleFunc("/control-light13", controlLight13).Methods("POST")
    router.HandleFunc("/control-light14", controlLight14).Methods("POST")
    router.HandleFunc("/control-light15", controlLight15).Methods("POST")

    fmt.Println("✅ Server on http://localhost:8080")
    go func() {
        log.Fatal(http.ListenAndServe(":8080", router))
    }()

    // 6) Create GUI + run
    createGUI()
    myWindow.ShowAndRun()

    serialPort.Close()
    fmt.Println("Program ended.")
}