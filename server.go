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
    "math"
    "math/rand"
    "net/http"
    "strings"
    "time"

    mqtt "github.com/eclipse/paho.mqtt.golang"
    "github.com/gorilla/mux"
    _ "github.com/lib/pq"
    "github.com/tarm/serial"

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

    myApp    fyne.App
    myWindow fyne.Window

    slider1, slider2, slider3                 *widget.Slider
    progressBar1, progressBar2, progressBar3  *widget.ProgressBar
    pumpOnButton, pumpOffButton               *widget.Button
    pumpStatus                                *canvas.Text

    // เก็บสถานะ pump และ LED brightness (13,14,15)
    currentPumpStatus bool
    led13Brightness   int
    led14Brightness   int
    led15Brightness   int
)

type AirData struct {
    Type        string  `json:"type"`
    AirID       int     `json:"air_id"`
    Temp        float64 `json:"temp"`
    AirHumidity float64 `json:"air_humidity"`
    PumpStatus  bool    `json:"pump_status"`
}

type SoilData struct {
    Type         string  `json:"type"`
    SoilID       int     `json:"soil_id"`
    SoilHumidity float64 `json:"soil_humidity"`
    PumpStatus   bool    `json:"pump_status"`
}

func mqttMessageHandler(client mqtt.Client, msg mqtt.Message) {
    fmt.Println("Received MQTT message on topic:", msg.Topic())
    fmt.Println("Payload:", string(msg.Payload()))
}

// ส่งค่า temp/humidity
func publishToMQTTAir(temp, hum float64) {
    if mqttClient == nil {
        fmt.Println("MQTT Client not initialized")
        return
    }
    payload := map[string]float64{
        "temperature":  temp,
        "air_humidity": hum,
    }
    b, err := json.Marshal(payload)
    if err != nil {
        fmt.Println("MQTT JSON marshal error:", err)
        return
    }
    token := mqttClient.Publish("smartfarm/sensors", 0, false, b)
    token.Wait()
    if token.Error() != nil {
        fmt.Println("Error publishing MQTT:", token.Error())
    } else {
        fmt.Println("Published to MQTT:", string(b))
    }
}

// ส่งค่า soil
func publishToMQTTSoil(soil float64) {
    if mqttClient == nil {
        fmt.Println("MQTT Client not initialized")
        return
    }
    payload := map[string]float64{
        "soil_humidity": soil,
    }
    b, err := json.Marshal(payload)
    if err != nil {
        fmt.Println("MQTT JSON marshal error:", err)
        return
    }
    token := mqttClient.Publish("smartfarm/sensors", 0, false, b)
    token.Wait()
    if token.Error() != nil {
        fmt.Println("Error publishing MQTT:", token.Error())
    } else {
        fmt.Println("Published to MQTT:", string(b))
    }
}

// อ่านค่า Serial
func readSerial() {
    reader := bufio.NewReader(serialPort)
    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            time.Sleep(time.Second)
            continue
        }
        line = strings.TrimSpace(line)

        // ตรวจว่าใน JSON มี \"type\":\"air\" หรือ \"type\":\"soil\" หรือเปล่า
        if !strings.Contains(line, "\"type\":\"") {
            fmt.Println("Unknown line:", line)
            continue
        }

        if strings.Contains(line, "\"type\":\"air\"") {
            var ad AirData
            e := json.Unmarshal([]byte(line), &ad)
            if e != nil {
                fmt.Println("JSON parse AirData error:", e, "line:", line)
                continue
            }
            // pump status จาก JSON => เก็บใน currentPumpStatus
            currentPumpStatus = ad.PumpStatus

            errA := insertAirValue(ad.AirID, ad.Temp, ad.AirHumidity)
            if errA != nil {
                fmt.Println("Insert airvalue error:", errA)
            } else {
                fmt.Printf("AirValue => air_id=%d, temp=%.1f, hum=%.1f\n", ad.AirID, ad.Temp, ad.AirHumidity)
                publishToMQTTAir(ad.Temp, ad.AirHumidity)
            }

        } else if strings.Contains(line, "\"type\":\"soil\"") {
            var sd SoilData
            e := json.Unmarshal([]byte(line), &sd)
            if e != nil {
                fmt.Println("JSON parse SoilData error:", e, "line:", line)
                continue
            }
            // ถ้าฝั่ง soil ส่ง pump_status มา ก็อาจเอามาใช้ได้เช่นกัน
            // currentPumpStatus = sd.PumpStatus

            errS := insertSoilValue(sd.SoilID, sd.SoilHumidity)
            if errS != nil {
                fmt.Println("Insert soilvalue error:", errS)
            } else {
                fmt.Printf("SoilValue => soil_id=%d, moisture=%.1f\n", sd.SoilID, sd.SoilHumidity)
                publishToMQTTSoil(sd.SoilHumidity)
            }
        } else {
            fmt.Println("Unknown type line:", line)
        }
    }
}

// insert air
func insertAirValue(airID int, temp, hum float64) error {
    _, err := db.Exec(
        `INSERT INTO airvalue (air_id, temp, air_humidity) VALUES ($1, $2, $3)`,
        airID, temp, hum,
    )
    return err
}

// insert soil
func insertSoilValue(soilID int, soil float64) error {
    _, err := db.Exec(
        `INSERT INTO soilvalue (soil_id, soil_humidity) VALUES ($1, $2)`,
        soilID, soil,
    )
    return err
}

// เสิร์ฟหน้า index.html
func serveHTML(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "index.html")
}

// โครงสร้าง JSON ที่จะส่งไปให้ Dashboard
func fetchSensorData(w http.ResponseWriter, r *http.Request) {
    type Response struct {
        Air1Temp     float64 `json:"air1_temp"`
        Air1Humidity float64 `json:"air1_humidity"`
        Air2Temp     float64 `json:"air2_temp"`
        Air2Humidity float64 `json:"air2_humidity"`
        SoilHumidity float64 `json:"soil_humidity"`
        PumpStatus   bool    `json:"pump_status"`
        LED1         int     `json:"led1"`
        LED2         int     `json:"led2"`
        LED3         int     `json:"led3"`
    }

    var res Response
    res.LED1 = led13Brightness
    res.LED2 = led14Brightness
    res.LED3 = led15Brightness

    // Query ล่าสุด air_id=1
    err := db.QueryRow(`SELECT temp, air_humidity FROM airvalue WHERE air_id=1 ORDER BY reading_time DESC LIMIT 1`).Scan(&res.Air1Temp, &res.Air1Humidity)
    if err != nil {
        fmt.Println("Error reading air_id=1:", err)
    }
    // Query ล่าสุด air_id=2
    err = db.QueryRow(`SELECT temp, air_humidity FROM airvalue WHERE air_id=2 ORDER BY reading_time DESC LIMIT 1`).Scan(&res.Air2Temp, &res.Air2Humidity)
    if err != nil {
        fmt.Println("Error reading air_id=2:", err)
    }

    // Query ล่าสุด soil_id=1
    err = db.QueryRow(`SELECT soil_humidity FROM soilvalue WHERE soil_id=1 ORDER BY reading_time DESC LIMIT 1`).Scan(&res.SoilHumidity)
    if err != nil {
        fmt.Println("Error reading soil_id=1:", err)
    }

    // ปั๊มน้ำ
    res.PumpStatus = currentPumpStatus

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(res)
}

// ควบคุม pump
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

// ควบคุมไฟ LED GPIO13
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
    led13Brightness = req.Brightness

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

// ควบคุมไฟ LED GPIO14
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
    led14Brightness = req.Brightness

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

// ควบคุมไฟ LED GPIO15
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
    led15Brightness = req.Brightness

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

func sendLight13Brightness(value int) {
    led13Brightness = value
    data := fmt.Sprintf(`{"brightness":%d}`, value)
    resp, err := http.Post("http://localhost:8080/control-light13", "application/json", bytes.NewBuffer([]byte(data)))
    if err != nil {
        fmt.Println("Error sending brightness 13:", err)
        return
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    fmt.Printf("Light13 Ack => %s\n", string(body))
}

func sendLight14Brightness(value int) {
    led14Brightness = value
    data := fmt.Sprintf(`{"brightness":%d}`, value)
    resp, err := http.Post("http://localhost:8080/control-light14", "application/json", bytes.NewBuffer([]byte(data)))
    if err != nil {
        fmt.Println("Error sending brightness 14:", err)
        return
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    fmt.Printf("Light14 Ack => %s\n", string(body))
}

func sendLight15Brightness(value int) {
    led15Brightness = value
    data := fmt.Sprintf(`{"brightness":%d}`, value)
    resp, err := http.Post("http://localhost:8080/control-light15", "application/json", bytes.NewBuffer([]byte(data)))
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
    resp, err := http.Post("http://localhost:8080/control-pump", "application/json", bytes.NewBuffer([]byte(data)))
    if err != nil {
        fmt.Println("Error pump command:", err)
        return
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    fmt.Printf("Pump Ack => %s\n", string(body))
}

func createGUI() {
    myApp = app.New()
    myWindow = myApp.NewWindow("Smart Farm Control Panel")
    bgRect := canvas.NewRectangle(color.NRGBA{R: 33, G: 33, B: 33, A: 255})
    messageBackground := canvas.NewRectangle(color.NRGBA{R: 230, G: 230, B: 230, A: 255})
    messageBackground.SetMinSize(fyne.NewSize(600, 35))
    title := canvas.NewText("Smart Farm Control Panel", color.Black)
    title.TextSize = 20
    title.TextStyle = fyne.TextStyle{Bold: true}
    titlebox := container.NewMax(messageBackground, container.NewCenter(title))

    brightness1Label := canvas.NewText("Brightness Outer 1", color.White)
    brightness1Label.TextSize = 16
    brightness1Label.TextStyle = fyne.TextStyle{Bold: true}
    slider1 = widget.NewSlider(0, 100)
    progressBar1 = widget.NewProgressBar()
    slider1.OnChanged = func(v float64) {
        fmt.Printf("Brightness Outer1 => %d%%\n", int(v))
        progressBar1.SetValue(v / 100)
        sendLight13Brightness(int(v))
    }

    brightness2Label := canvas.NewText("Brightness Outer 2", color.White)
    brightness2Label.TextSize = 16
    brightness2Label.TextStyle = fyne.TextStyle{Bold: true}
    slider2 = widget.NewSlider(0, 100)
    progressBar2 = widget.NewProgressBar()
    slider2.OnChanged = func(v float64) {
        fmt.Printf("Brightness Outer2 => %d%%\n", int(v))
        progressBar2.SetValue(v / 100)
        sendLight14Brightness(int(v))
    }

    brightness3Label := canvas.NewText("Brightness Inner", color.White)
    brightness3Label.TextSize = 16
    brightness3Label.TextStyle = fyne.TextStyle{Bold: true}
    slider3 = widget.NewSlider(0, 100)
    progressBar3 = widget.NewProgressBar()
    slider3.OnChanged = func(v float64) {
        fmt.Printf("Brightness Inner => %d%%\n", int(v))
        progressBar3.SetValue(v / 100)
        sendLight15Brightness(int(v))
    }

    waterPumpLabel := canvas.NewText("Water Pump", color.White)
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
    var err error
    db, err = sql.Open("postgres", dbConn)
    if err != nil {
        log.Fatal("DB Error:", err)
    }
    defer db.Close()

    cfg := &serial.Config{Name: "/dev/tty.usbmodem1201", Baud: 115200, ReadTimeout: 2 * time.Second}
    serialPort, err = serial.OpenPort(cfg)
    if err != nil {
        log.Fatal("Error opening serial:", err)
    }
    fmt.Println("Serial opened")

    opts := mqtt.NewClientOptions()
    opts.AddBroker("tcp://localhost:1883")
    opts.SetClientID("smartfarmGoClient")
    opts.OnConnect = func(c mqtt.Client) {
        fmt.Println("MQTT Client connected")
    }
    opts.SetDefaultPublishHandler(mqttMessageHandler)

    mqttClient = mqtt.NewClient(opts)
    token := mqttClient.Connect()
    token.Wait()
    if token.Error() != nil {
        log.Fatal("MQTT connect error:", token.Error())
    }

    go readSerial()

    router := mux.NewRouter()
    router.HandleFunc("/", serveHTML)
    router.HandleFunc("/sensor-data", fetchSensorData).Methods("GET")
    router.HandleFunc("/control-pump", controlPump).Methods("POST")
    router.HandleFunc("/control-light13", controlLight13).Methods("POST")
    router.HandleFunc("/control-light14", controlLight14).Methods("POST")
    router.HandleFunc("/control-light15", controlLight15).Methods("POST")

    // เสิร์ฟไฟล์ static (index.html, styles.css, script.js) ให้โหลดได้
    router.PathPrefix("/").Handler(http.StripPrefix("/", http.FileServer(http.Dir("."))))

    fmt.Println("Server on http://localhost:8080")
    go func() {
        log.Fatal(http.ListenAndServe(":8080", router))
    }()

    // ตัวอย่าง: จำลองการ insert air_id=2 แบบสุ่ม temp/humidity ทุก 5 วิ
    go func() {
        for {
            temp := math.Round(24.0+((rand.Float64()*4.0*10)/10)) 
            hum := math.Round(40.0+((rand.Float64()*10.0*10)/10))
            err := insertAirValue(2, temp, hum)
            if err != nil {
                fmt.Println("❌ Simulated insertAirValue (air_id=2) error:", err)
            } else {
                fmt.Printf("✅ Simulated AirValue => air_id=2, temp=%.1f, hum=%.1f\n", temp, hum)
                publishToMQTTAir(temp, hum)
            }
            time.Sleep(5 * time.Second)
        }
    }()

    createGUI()
    myWindow.ShowAndRun()

    serialPort.Close()
    mqttClient.Disconnect(250)
    fmt.Println("Program ended.")
}