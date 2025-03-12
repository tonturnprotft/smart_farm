package main

import (
    "fmt"
    "machine"
    "strconv"
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
    // Serial & Pump
    serial = machine.Serial
    relay1 = machine.GP3
    relay2 = machine.GP4
    pumpOn bool

    // ความถี่ PWM (1 kHz)
    freqHz = uint64(1000)

    // PWM A = GPIO13 (slice6)
    pwmA  = machine.PWM6
    pinA  = machine.GPIO13
    chA   uint8

    // PWM B/C = GPIO14, GPIO15 (slice7)
    pwmB = machine.PWM7
    pinB = machine.GPIO14
    chB  uint8

    pinC = machine.GPIO15
    chC  uint8

    // หากอยากเก็บ brightness แยก
    lightDuty13 uint32
    lightDuty14 uint32
    lightDuty15 uint32
)

func main() {
    fmt.Println("🚀 Pico: Separate PWM Control for GPIO13,14,15")

    // ตั้งค่า Serial
    serial.Configure(machine.UARTConfig{BaudRate: 115200})

    // ตั้งค่า Pump (Relay)
    relay1.Configure(machine.PinConfig{Mode: machine.PinOutput})
    relay2.Configure(machine.PinConfig{Mode: machine.PinOutput})
    relay1.High() // ปิดปั๊ม
    relay2.High()
    pumpOn = false

    // ตั้งค่า PWM A: GPIO13
    period := uint64(1e9 / freqHz)
    if err := pwmA.Configure(machine.PWMConfig{Period: period}); err != nil {
        fmt.Printf("❌ PWM6 (GPIO13) configure error: %v\n", err)
    }
    aCh, errA := pwmA.Channel(pinA)
    if errA != nil {
        fmt.Printf("❌ PWM6 channel A error (GPIO13): %v\n", errA)
    }
    pinA.Configure(machine.PinConfig{Mode: machine.PinPWM})
    chA = aCh
    pwmA.Set(chA, 0)
    lightDuty13 = 0

    // ตั้งค่า PWM B/C: GPIO14,15
    if err := pwmB.Configure(machine.PWMConfig{Period: period}); err != nil {
        fmt.Printf("❌ PWM7 configure error: %v\n", err)
    }
    bCh, errB := pwmB.Channel(pinB)
    if errB != nil {
        fmt.Printf("❌ PWM7 channel B error (GPIO14): %v\n", errB)
    }
    pinB.Configure(machine.PinConfig{Mode: machine.PinPWM})
    chB = bCh
    pwmB.Set(chB, 0)
    lightDuty14 = 0

    cCh, errC := pwmB.Channel(pinC)
    if errC != nil {
        fmt.Printf("❌ PWM7 channel C error (GPIO15): %v\n", errC)
    }
    pinC.Configure(machine.PinConfig{Mode: machine.PinPWM})
    chC = cCh
    pwmB.Set(chC, 0)
    lightDuty15 = 0

    fmt.Println("✅ PWM on GPIO13,14,15 = 0% initially")

    // ตั้งค่า Sensor
    dhtSensor := dht.New(machine.GP2, dht.DHT22)
    machine.InitADC()
    adc := machine.ADC{Pin: machine.GP27}
    adc.Configure(machine.ADCConfig{})

    for {
        // อ่านเซ็นเซอร์
        temp, hum, err := dhtSensor.Measurements()
        if err != nil {
            // อ่านไม่ได้ก็ข้าม
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
        fmt.Println(js)

        // ถ้ามีคำสั่งจาก Serial
        if serial.Buffered() > 0 {
            cmd := readLine()
            cmd = strings.TrimSpace(cmd)
            fmt.Printf("[DEBUG] Received cmd = %q\n", cmd)

            switch {
            case cmd == "on":
                // ปั๊มน้ำ
                fmt.Println("[DEBUG] Cmd == on → relay1.Low()")
                relay1.Low()
                relay2.Low()
                pumpOn = true
                serial.Write([]byte("ACK: Pump ON\n"))

            case cmd == "off":
                fmt.Println("[DEBUG] Cmd == off → relay1.High()")
                relay1.High()
                relay2.High()
                pumpOn = false
                serial.Write([]byte("ACK: Pump OFF\n"))

            // light13:NN → GPIO13
            case strings.HasPrefix(cmd, "light13:"):
                valStr := strings.TrimPrefix(cmd, "light13:")
                valStr = strings.TrimSpace(valStr)
                val, err := strconv.Atoi(valStr)
                if err == nil {
                    if val < 0 { val = 0 }
                    if val > 100 { val = 100 }
                    lightDuty13 = uint32(val)
                    dutyA := pwmA.Top() * lightDuty13 / 100
                    pwmA.Set(chA, dutyA)

                    ack := fmt.Sprintf("ACK: light13=%d\n", val)
                    serial.Write([]byte(ack))
                } else {
                    serial.Write([]byte("ERR: invalid brightness\n"))
                }

            // light14:NN → GPIO14
            case strings.HasPrefix(cmd, "light14:"):
                valStr := strings.TrimPrefix(cmd, "light14:")
                valStr = strings.TrimSpace(valStr)
                val, err := strconv.Atoi(valStr)
                if err == nil {
                    if val < 0 { val = 0 }
                    if val > 100 { val = 100 }
                    lightDuty14 = uint32(val)
                    dutyB := pwmB.Top() * lightDuty14 / 100
                    pwmB.Set(chB, dutyB)

                    ack := fmt.Sprintf("ACK: light14=%d\n", val)
                    serial.Write([]byte(ack))
                } else {
                    serial.Write([]byte("ERR: invalid brightness\n"))
                }

            // light15:NN → GPIO15
            case strings.HasPrefix(cmd, "light15:"):
                valStr := strings.TrimPrefix(cmd, "light15:")
                valStr = strings.TrimSpace(valStr)
                val, err := strconv.Atoi(valStr)
                if err == nil {
                    if val < 0 { val = 0 }
                    if val > 100 { val = 100 }
                    lightDuty15 = uint32(val)
                    dutyC := pwmB.Top() * lightDuty15 / 100
                    pwmB.Set(chC, dutyC)

                    ack := fmt.Sprintf("ACK: light15=%d\n", val)
                    serial.Write([]byte(ack))
                } else {
                    serial.Write([]byte("ERR: invalid brightness\n"))
                }

            default:
                serial.Write([]byte("ERR: Unknown\n"))
            }
        }

        time.Sleep(500 * time.Millisecond)
    }
}

// ฟังก์ชันแปลง struct → JSON (เรียบง่าย)
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