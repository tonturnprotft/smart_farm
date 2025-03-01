package main

import (
    "fmt"
    "machine"
    "strings"
    "time"
    "tinygo.org/x/drivers/dht"
)

// โครงสร้างข้อมูลเซ็นเซอร์ (ส่งออกเป็น JSON)
type SensorData struct {
    Temperature  float64 `json:"temperature"`
    Humidity     float64 `json:"humidity"`
    SoilMoisture float64 `json:"soil_moisture"`
    PumpStatus   bool    `json:"pump_status"`
}

var (
    serial = machine.Serial
    relay1 = machine.GP3
    relay2 = machine.GP4

    pumpOn bool
)

func main() {
    fmt.Println("🚀 Pico: Start program")

    // ตั้งค่า Serial
    serial.Configure(machine.UARTConfig{BaudRate: 115200})

    // ตั้งค่า Relay Pins
    relay1.Configure(machine.PinConfig{Mode: machine.PinOutput})
    relay2.Configure(machine.PinConfig{Mode: machine.PinOutput})
    relay1.High() // ปิดปั๊ม
    relay2.High()
    pumpOn = false

    // ตั้งค่า Sensor
    dhtSensor := dht.New(machine.GP2, dht.DHT22)
    machine.InitADC()
    adc := machine.ADC{Pin: machine.GP27}
    adc.Configure(machine.ADCConfig{})

    for {
        // อ่านเซ็นเซอร์ (mock)
        temp, hum, err := dhtSensor.Measurements()
        if err != nil {
            // ถ้าอ่านไม่ได้ก็ข้าม
        }
        soilRaw := adc.Get()
        soilMoisture := 100 - ((float32(soilRaw) / 65535) * 100)

        // ส่งข้อมูลเซ็นเซอร์เป็น JSON ทาง Serial
        data := SensorData{
            Temperature:  float64(temp) / 10,
            Humidity:     float64(hum) / 10,
            SoilMoisture: float64(soilMoisture),
            PumpStatus:   pumpOn,
        }
        js := toJSON(data)
        fmt.Println(js) // พิมพ์ออกทาง Serial

        // ถ้ามีคำสั่งจาก Server → อ่าน
        if serial.Buffered() > 0 {
            cmd := readLine()
            cmd = strings.TrimSpace(cmd)
            fmt.Printf("[DEBUG] Received cmd = %q\n", cmd)
            if cmd == "on" {
                fmt.Println("[DEBUG] Cmd == on → relay1.Low()")
                relay1.Low()
                relay2.Low()
                pumpOn = true
                serial.Write([]byte("ACK: Pump ON\n"))
            } else if cmd == "off" {
                fmt.Println("[DEBUG] Cmd == off → relay1.High()")
                relay1.High()
                relay2.High()
                pumpOn = false
                serial.Write([]byte("ACK: Pump OFF\n"))
            } else {
                serial.Write([]byte("ERR: Unknown\n"))
            }
        }

        time.Sleep(500 * time.Millisecond)
    }
}

// ฟังก์ชันแปลง struct → JSON (เล็กๆ)
func toJSON(d SensorData) string {
    return fmt.Sprintf(`{"temperature":%.1f,"humidity":%.1f,"soil_moisture":%.1f,"pump_status":%t}`,
        d.Temperature, d.Humidity, d.SoilMoisture, d.PumpStatus)
}

// อ่านทีละไบต์จนเจอ '\n'
func readLine() string {
    buf := make([]byte, 32)
    i := 0
    for {
        if serial.Buffered() == 0 {
            time.Sleep(10 * time.Millisecond)
            if serial.Buffered() == 0 {
                break
            }
        }
        b, err := serial.ReadByte()
        if err != nil {
            break
        }
        if b == '\n' || i >= len(buf)-1 {
            break
        }
        buf[i] = b
        i++
    }
    return string(buf[:i])
}