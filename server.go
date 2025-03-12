package main

import (
    "bufio"
    "bytes"
    "database/sql"
    "encoding/json"
    "fmt"
    "image/color"
    "io/ioutil"
    "log"
    "net/http"
    "strings"
    "time"

    mqtt "github.com/eclipse/paho.mqtt.golang"
    "github.com/gorilla/mux"
    _ "github.com/lib/pq"
    "github.com/tarm/serial"

    // We'll add endpoints for control-light13,14,15 and 3 separate sliders
    "fyne.io/fyne/v2"
    "fyne.io/fyne/v2/app"
    "fyne.io/fyne/v2/canvas"
    "fyne.io/fyne/v2/container"
    "fyne.io/fyne/v2/layout"
    "fyne.io/fyne/v2/widget"
)

const dbConn = "user=postgres password=4847 dbname=farm_db sslmode=disable"

var (
    db             *sql.DB
    serialPort     *serial.Port
    mqttClient     mqtt.Client

    // เก็บสถานะปั๊มและความสว่างปัจจุบันไว้ที่ Server
    currentPumpStatus   = "off"
    currentBrightness   = 0
)

// We'll store brightness separate for each pin if needed
// ---------------------- Data Structures --------------------------
type SensorData struct {
    Temperature  float64 `json:"temperature"`
    AirHumidity  float64 `json:"humidity"`
    SoilMoisture float64 `json:"soil_moisture"`
    Brightness   float64 `json:"brightness"`
}

// ------------------- MQTT Handler & Publish ----------------------
func mqttMessageHandler(client mqtt.Client, msg mqtt.Message) {
    fmt.Println("📡 Incoming MQTT Data:", string(msg.Payload()))
    // ถ้ามีโค้ด parse แล้วเก็บลง DB ก็ทำได้
}

// ฟังก์ชัน Publish pump_status + brightness เป็น JSON เดียว
func publishPumpAndBrightnessMQTT(pumpStatus string, brightness int) {
    if mqttClient == nil {
        fmt.Println("❌ MQTT Client not initialized")
        return
    }
    data := map[string]interface{}{
        "pump_status": pumpStatus,
        "brightness":  brightness,
    }
    b, _ := json.Marshal(data)
    token := mqttClient.Publish("smartfarm/dashboard", 0, false, b)
    token.Wait()
    if token.Error() != nil {
        fmt.Println("❌ MQTT Publish Error:", token.Error())
    } else {
        fmt.Println("✅ Sent to MQTT (Pump+Light):", string(b))
    }
}

// ------------------- Read Serial & Insert Sensor ---------------
func readSerial() {
    reader := bufio.NewReader(serialPort)
    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            // EOF หรือไม่มีข้อมูล
            fmt.Println("No input right now", err)
            time.Sleep(time.Second)
            continue
        }
        line = strings.TrimSpace(line)

        // ถ้าไม่ใช่ JSON sensor { ... }, ข้าม
        if !strings.HasPrefix(line, "{") {
            fmt.Println("Non-JSON line:", line)
            continue
        }
        var data SensorData
        if err := json.Unmarshal([]byte(line), &data); err != nil {
            fmt.Println("❌ JSON Parsing Error:", err, "| line:", line)
            continue
        }
        // Insert DB sensor data (ยกเว้น pump_status)
        _, err = db.Exec(`INSERT INTO sensor (date, time, temp, air_humidity, soil_humidity, brightness)
            VALUES (CURRENT_DATE, CURRENT_TIME, $1, $2, $3, $4)`,
            data.Temperature, data.AirHumidity, data.SoilMoisture, data.Brightness)
        if err != nil {
            fmt.Println("❌ DB Error:", err)
        } else {
            fmt.Println("✅ Sensor Data Stored:", data)
        }
    }
}

// ------------------- Web APIs -----------------------------------
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

func serveHTML(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "index.html")
}

// ควบคุมปั๊มน้ำ (on/off) แล้ว publish MQTT + ไม่บันทึก pump_status ใน DB
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
    // เขียนลง Serial
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

    // อัปเดต currentPumpStatus
    currentPumpStatus = req.Command // on/off
    // ส่ง MQTT รวม brightness ปัจจุบัน
    publishPumpAndBrightnessMQTT(currentPumpStatus, currentBrightness)

    // ตอบกลับ
    resp := map[string]string{"ack": strings.TrimSpace(ackLine)}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

// ควบคุมความสว่าง (brightness 0..100) -> insert DB -> publish MQTT
func controlLight(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Brightness int `json:"brightness"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }
    if req.Brightness < 0 || req.Brightness > 100 {
        http.Error(w, "Brightness must be 0..100", http.StatusBadRequest)
        return
    }
    // เขียนลง Serial เช่น light:NN
    cmd := fmt.Sprintf("light:%d\n", req.Brightness)
    _, err := serialPort.Write([]byte(cmd))
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

    // Insert brightness ลง DB (ตัวอย่าง insert table brightness_log)
    _, err = db.Exec(`INSERT INTO brightness_log (date, time, brightness) 
                      VALUES (CURRENT_DATE, CURRENT_TIME, $1)`, req.Brightness)
    if err != nil {
        fmt.Println("❌ Insert brightness DB Error:", err)
    } else {
        fmt.Println("✅ Insert brightness =", req.Brightness)
    }

    // อัปเดตตัวแปร currentBrightness
    currentBrightness = req.Brightness
    // ส่ง MQTT (pump_status + brightness)
    publishPumpAndBrightnessMQTT(currentPumpStatus, currentBrightness)

    // ตอบกลับ
    resp := map[string]string{"ack": strings.TrimSpace(ackLine)}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

// ========== New functions for controlling 3 separate pins ==========
func controlLight13(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Brightness int `json:"brightness"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }
    if req.Brightness < 0 || req.Brightness > 100 {
        http.Error(w, "Brightness must be 0..100", http.StatusBadRequest)
        return
    }
    // สร้างคำสั่ง เช่น light13:NN
    cmd := fmt.Sprintf("light13:%d\n", req.Brightness)
    _, err := serialPort.Write([]byte(cmd))
    if err != nil {
        http.Error(w, "Failed to write serial", http.StatusInternalServerError)
        return
    }
    serialPort.Flush()
    ackReader := bufio.NewReader(serialPort)
    ackLine, err := ackReader.ReadString('\n')
    if err != nil {
        http.Error(w, "No ACK or read error", http.StatusGatewayTimeout)
        return
    }
    // ตอบกลับ
    resp := map[string]string{"ack": strings.TrimSpace(ackLine)}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func controlLight14(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Brightness int `json:"brightness"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }
    if req.Brightness < 0 || req.Brightness > 100 {
        http.Error(w, "Brightness must be 0..100", http.StatusBadRequest)
        return
    }
    // คำสั่งเช่น light14:NN
    cmd := fmt.Sprintf("light14:%d\n", req.Brightness)
    _, err := serialPort.Write([]byte(cmd))
    if err != nil {
        http.Error(w, "Failed to write serial", http.StatusInternalServerError)
        return
    }
    serialPort.Flush()
    ackReader := bufio.NewReader(serialPort)
    ackLine, err := ackReader.ReadString('\n')
    if err != nil {
        http.Error(w, "No ACK or read error", http.StatusGatewayTimeout)
        return
    }
    resp := map[string]string{"ack": strings.TrimSpace(ackLine)}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func controlLight15(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Brightness int `json:"brightness"`
    }
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
    ackReader := bufio.NewReader(serialPort)
    ackLine, err := ackReader.ReadString('\n')
    if err != nil {
        http.Error(w, "No ACK or read error", http.StatusGatewayTimeout)
        return
    }
    resp := map[string]string{"ack": strings.TrimSpace(ackLine)}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

// ------------------- GUI ----------------------------------------
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
        _, err := sendPumpCommand("on")
        if err != nil {
            pumpStatus.Text = fmt.Sprintf("Error: %v", err)
        } else {
            pumpStatus.Text = "Pump is on"
        }
        pumpOnButton.Disable()
        pumpOffButton.Enable()
        myWindow.Canvas().Refresh(pumpStatus)
    })

    pumpOffButton = widget.NewButton("Turn Off", func() {
        _, err := sendPumpCommand("off")
        if err != nil {
            pumpStatus.Text = fmt.Sprintf("Error: %v", err)
        } else {
            pumpStatus.Text = "Pump is off"
        }
        pumpOffButton.Disable()
        pumpOnButton.Enable()
        myWindow.Canvas().Refresh(pumpStatus)
    })
    pumpOffButton.Disable()

    // ========== New: 3 sliders for light13, light14, light15 ==========
    lightStatus13 := canvas.NewText("GPIO13 Brightness = 0", color.White)
    lightStatus13.TextSize = 14
    lightStatus13.TextStyle.Bold = true
    slider13 := widget.NewSlider(0, 100)
    slider13.OnChanged = func(value float64) {
        brightness := int(value)
        _, err := sendLight13Brightness(brightness)
        if err != nil {
            lightStatus13.Text = fmt.Sprintf("Error: %v", err)
        } else {
            lightStatus13.Text = fmt.Sprintf("GPIO13 Brightness = %d", brightness)
        }
        myWindow.Canvas().Refresh(lightStatus13)
    }

    lightStatus14 := canvas.NewText("GPIO14 Brightness = 0", color.White)
    lightStatus14.TextSize = 14
    lightStatus14.TextStyle.Bold = true
    slider14 := widget.NewSlider(0, 100)
    slider14.OnChanged = func(value float64) {
        brightness := int(value)
        _, err := sendLight14Brightness(brightness)
        if err != nil {
            lightStatus14.Text = fmt.Sprintf("Error: %v", err)
        } else {
            lightStatus14.Text = fmt.Sprintf("GPIO14 Brightness = %d", brightness)
        }
        myWindow.Canvas().Refresh(lightStatus14)
    }

    lightStatus15 := canvas.NewText("GPIO15 Brightness = 0", color.White)
    lightStatus15.TextSize = 14
    lightStatus15.TextStyle.Bold = true
    slider15 := widget.NewSlider(0, 100)
    slider15.OnChanged = func(value float64) {
        brightness := int(value)
        _, err := sendLight15Brightness(brightness)
        if err != nil {
            lightStatus15.Text = fmt.Sprintf("Error: %v", err)
        } else {
            lightStatus15.Text = fmt.Sprintf("GPIO15 Brightness = %d", brightness)
        }
        myWindow.Canvas().Refresh(lightStatus15)
    }

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

        // GPIO13
        container.NewCenter(lightStatus13),
        slider13,

        // GPIO14
        container.NewCenter(lightStatus14),
        slider14,

        // GPIO15
        container.NewCenter(lightStatus15),
        slider15,
    )

    myWindow.SetContent(content)
    myWindow.ShowAndRun()
}

// ----------------- Helper for Pump / Light commands ------------
func sendPumpCommand(cmd string) (string, error) {
    data := fmt.Sprintf(`{"command":"%s"}`, cmd)
    resp, err := http.Post("http://localhost:8080/control-pump",
        "application/json",
        bytes.NewBuffer([]byte(data)))
    if err != nil {
        return "", fmt.Errorf("HTTP POST Error: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := ioutil.ReadAll(resp.Body)
        return "", fmt.Errorf("server error [%d]: %s", resp.StatusCode, string(body))
    }
    b, _ := ioutil.ReadAll(resp.Body)
    var result map[string]string
    if err := json.Unmarshal(b, &result); err != nil {
        return "", err
    }
    return result["ack"], nil
}

func sendLightBrightness(val int) (string, error) {
    data := fmt.Sprintf(`{"brightness":%d}`, val)
    resp, err := http.Post("http://localhost:8080/control-light",
        "application/json",
        bytes.NewBuffer([]byte(data)))
    if err != nil {
        return "", fmt.Errorf("HTTP POST Error: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := ioutil.ReadAll(resp.Body)
        return "", fmt.Errorf("server error [%d]: %s", resp.StatusCode, string(body))
    }
    b, _ := ioutil.ReadAll(resp.Body)
    var result map[string]string
    if err := json.Unmarshal(b, &result); err != nil {
        return "", err
    }
    return result["ack"], nil
}

// New separate sendLight13Brightness, sendLight14Brightness, sendLight15Brightness
func sendLight13Brightness(val int) (string, error) {
    data := fmt.Sprintf(`{"brightness":%d}`, val)
    resp, err := http.Post("http://localhost:8080/control-light13",
        "application/json",
        bytes.NewBuffer([]byte(data)))
    if err != nil {
        return "", fmt.Errorf("HTTP POST Error: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := ioutil.ReadAll(resp.Body)
        return "", fmt.Errorf("server error [%d]: %s", resp.StatusCode, string(body))
    }
    b, _ := ioutil.ReadAll(resp.Body)
    var result map[string]string
    if err := json.Unmarshal(b, &result); err != nil {
        return "", err
    }
    return result["ack"], nil
}

func sendLight14Brightness(val int) (string, error) {
    data := fmt.Sprintf(`{"brightness":%d}`, val)
    resp, err := http.Post("http://localhost:8080/control-light14",
        "application/json",
        bytes.NewBuffer([]byte(data)))
    if err != nil {
        return "", fmt.Errorf("HTTP POST Error: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := ioutil.ReadAll(resp.Body)
        return "", fmt.Errorf("server error [%d]: %s", resp.StatusCode, string(body))
    }
    b, _ := ioutil.ReadAll(resp.Body)
    var result map[string]string
    if err := json.Unmarshal(b, &result); err != nil {
        return "", err
    }
    return result["ack"], nil
}

func sendLight15Brightness(val int) (string, error) {
    data := fmt.Sprintf(`{"brightness":%d}`, val)
    resp, err := http.Post("http://localhost:8080/control-light15",
        "application/json",
        bytes.NewBuffer([]byte(data)))
    if err != nil {
        return "", fmt.Errorf("HTTP POST Error: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := ioutil.ReadAll(resp.Body)
        return "", fmt.Errorf("server error [%d]: %s", resp.StatusCode, string(body))
    }
    b, _ := ioutil.ReadAll(resp.Body)
    var result map[string]string
    if err := json.Unmarshal(b, &result); err != nil {
        return "", err
    }
    return result["ack"], nil
}

// ----------------- main -----------------------------------------
func main() {
    var err error
    db, err = sql.Open("postgres", dbConn)
    if err != nil {
        log.Fatal("DB Error:", err)
    }
    defer db.Close()

    // เปิด Serial
    c := &serial.Config{Name: "/dev/tty.usbmodem1301", Baud: 115200, ReadTimeout: 2 * time.Second}
    serialPort, err = serial.OpenPort(c)
    if err != nil {
        log.Fatal("❌ Error opening serial port:", err)
    }
    fmt.Println("✅ Serial port opened!")

    // เชื่อม MQTT
    opts := mqtt.NewClientOptions().AddBroker("tcp://localhost:1883")
    mqttClient = mqtt.NewClient(opts)
    tok := mqttClient.Connect()
    tok.Wait()
    if tok.Error() != nil {
        log.Fatal("❌ MQTT connect error:", tok.Error())
    }
    fmt.Println("✅ MQTT Client connected")

    // อ่าน Sensor
    go readSerial()

    // Web Server
    router := mux.NewRouter()
    router.HandleFunc("/", serveHTML)
    router.HandleFunc("/sensor-data", fetchSensorData).Methods("GET")
    router.HandleFunc("/control-pump", controlPump).Methods("POST")
    //router.HandleFunc("/control-light", controlLight).Methods("POST") // เดิม: single light

    // =========== เพิ่ม 3 endpoint แยกสำหรับ light13, light14, light15 ============
    router.HandleFunc("/control-light13", controlLight13).Methods("POST")
    router.HandleFunc("/control-light14", controlLight14).Methods("POST")
    router.HandleFunc("/control-light15", controlLight15).Methods("POST")

    fmt.Println("✅ Server running on http://localhost:8080")
    go func() {
        log.Fatal(http.ListenAndServe(":8080", router))
    }()

    // GUI
    createGUI()

    serialPort.Close()
    mqttClient.Disconnect(250)
}