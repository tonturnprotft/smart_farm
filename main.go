package main

import (
    "fmt"
    "machine"
    "math"
    "strconv"
    "strings"
    "time"

    "tinygo.org/x/drivers/dht"
)

// Threshold สำหรับส่งข้อมูลเมื่อเปลี่ยนเกินค่านี้
const (
    tempThreshold = 0.2
    humThreshold  = 0.5
    soilThreshold = 1.0
)

// โครงสร้างสำหรับส่ง JSON 2 แบบ
// - toJSONAir =>  {\"type\":\"air\",\"air_id\":...,\"temp\":...,\"air_humidity\":...,\"pump_status\":bool}
// - toJSONSoil => {\"type\":\"soil\",\"soil_id\":...,\"soil_humidity\":...,\"pump_status\":bool}

var (
    // Serial & Pump
    serial = machine.Serial
    relay1 = machine.GP3
    relay2 = machine.GP4
    pumpOn bool

    // ความถี่ PWM (1 kHz)
    freqHz = uint64(1000)

    // PWM สำหรับไฟ: สมมติ Outer1=GPIO13, Outer2=GPIO14, Inner=GPIO15
    pwmA  = machine.PWM6
    pinA  = machine.GPIO13
    chA   uint8

    pwmB = machine.PWM7
    pinB = machine.GPIO14
    chB  uint8

    pinC = machine.GPIO15
    chC  uint8

    lightDuty13 uint32
    lightDuty14 uint32
    lightDuty15 uint32
)

// =============== MAIN LOOP ===============
func main() {
    fmt.Println("🚀 Pico multi-sensor: Air & Soil, separate JSON")

    // ตั้งค่า Serial
    serial.Configure(machine.UARTConfig{BaudRate: 115200})

    // ตั้งค่า Pump (Relay)
    relay1.Configure(machine.PinConfig{Mode: machine.PinOutput})
    relay2.Configure(machine.PinConfig{Mode: machine.PinOutput})
    relay1.High()
    relay2.High()
    pumpOn = false

    // ตั้งค่า PWM
    period := uint64(1e9 / freqHz)
    if err := pwmA.Configure(machine.PWMConfig{Period: period}); err != nil {
        fmt.Printf("❌ PWM6 (GPIO13) error: %v\n", err)
    }
    aCh, errA := pwmA.Channel(pinA)
    if errA != nil {
        fmt.Printf("❌ channel A (GPIO13) error: %v\n", errA)
    }
    pinA.Configure(machine.PinConfig{Mode: machine.PinPWM})
    chA = aCh
    pwmA.Set(chA, 0)
    lightDuty13 = 0

    if err := pwmB.Configure(machine.PWMConfig{Period: period}); err != nil {
        fmt.Printf("❌ PWM7 error: %v\n", err)
    }
    bCh, errB := pwmB.Channel(pinB)
    if errB != nil {
        fmt.Printf("❌ channel B (GPIO14) error: %v\n", errB)
    }
    pinB.Configure(machine.PinConfig{Mode: machine.PinPWM})
    chB = bCh
    pwmB.Set(chB, 0)
    lightDuty14 = 0

    cCh, errC := pwmB.Channel(pinC)
    if errC != nil {
        fmt.Printf("❌ channel C (GPIO15) error: %v\n", errC)
    }
    pinC.Configure(machine.PinConfig{Mode: machine.PinPWM})
    chC = cCh
    pwmB.Set(chC, 0)
    lightDuty15 = 0

    fmt.Println("✅ PWM on GPIO13,14,15 = 0% initially")

    // ตั้งค่าเซ็นเซอร์: DHT22 => Air, ADC => Soil
    dhtSensor := dht.New(machine.GP16, dht.DHT22) // สมมติ GP16
    machine.InitADC()
    adc := machine.ADC{Pin: machine.GP27}
    adc.Configure(machine.ADCConfig{})

    var lastTemp, lastHum, lastSoil float64
    firstReading := true

    for {
        // อ่าน DHT
        tRaw, hRaw, dhtErr := dhtSensor.Measurements()
        if dhtErr != nil {
            // ถ้า error ก็ข้าม
        }
        newTemp := float64(tRaw) / 10.0
        newHum  := float64(hRaw) / 10.0

        // อ่าน Soil
        soilRaw := adc.Get()
        newSoil := float64(100 - ((float32(soilRaw) / 65535) * 100))

        if firstReading {
            // ครั้งแรก ส่ง 2 JSON เลย
            fmt.Println(toJSONAir(1, newTemp, newHum, pumpOn))
            fmt.Println(toJSONSoil(1, newSoil, pumpOn))
            lastTemp = newTemp
            lastHum  = newHum
            lastSoil = newSoil
            firstReading = false
        } else {
            // threshold แยก
            airChanged  := changedBeyondThreshold(lastTemp, newTemp, tempThreshold) ||
                          changedBeyondThreshold(lastHum,  newHum,  humThreshold)
            soilChanged := changedBeyondThreshold(lastSoil, newSoil, soilThreshold)

            if airChanged {
                fmt.Println(toJSONAir(1, newTemp, newHum, pumpOn))
                lastTemp = newTemp
                lastHum  = newHum
            }
            if soilChanged {
                fmt.Println(toJSONSoil(1, newSoil, pumpOn))
                lastSoil = newSoil
            }
        }

        // ถ้ามีคำสั่งจาก Serial
        if serial.Buffered() > 0 {
            cmd := readLine()
            cmd = strings.TrimSpace(cmd)
            fmt.Printf("[DEBUG] Received cmd = %q\n", cmd)

            switch {
            case cmd == "on":
                fmt.Println("[DEBUG] relay1.Low() / relay2.Low() => Pump ON")
                relay1.Low()
                relay2.Low()
                pumpOn = true
                serial.Write([]byte("ACK: Pump ON\n"))

            case cmd == "off":
                fmt.Println("[DEBUG] relay1.High() / relay2.High() => Pump OFF")
                relay1.High()
                relay2.High()
                pumpOn = false
                serial.Write([]byte("ACK: Pump OFF\n"))

            case strings.HasPrefix(cmd, "light13:"):
                valStr := strings.TrimPrefix(cmd, "light13:")
                val, e := strconv.Atoi(strings.TrimSpace(valStr))
                if e == nil {
                    lightDuty13 = clampValue(val)
                    dutyA := pwmA.Top() * lightDuty13 / 100
                    pwmA.Set(chA, dutyA)
                    ack := fmt.Sprintf("ACK: light13=%d\n", val)
                    serial.Write([]byte(ack))
                }

            case strings.HasPrefix(cmd, "light14:"):
                valStr := strings.TrimPrefix(cmd, "light14:")
                val, e := strconv.Atoi(strings.TrimSpace(valStr))
                if e == nil {
                    lightDuty14 = clampValue(val)
                    dutyB := pwmB.Top() * lightDuty14 / 100
                    pwmB.Set(chB, dutyB)
                    ack := fmt.Sprintf("ACK: light14=%d\n", val)
                    serial.Write([]byte(ack))
                }

            case strings.HasPrefix(cmd, "light15:"):
                valStr := strings.TrimPrefix(cmd, "light15:")
                val, e := strconv.Atoi(strings.TrimSpace(valStr))
                if e == nil {
                    lightDuty15 = clampValue(val)
                    dutyC := pwmB.Top() * lightDuty15 / 100
                    pwmB.Set(chC, dutyC)
                    ack := fmt.Sprintf("ACK: light15=%d\n", val)
                    serial.Write([]byte(ack))
                }

            default:
                serial.Write([]byte("ERR: Unknown\n"))
            }
        }

        time.Sleep(500 * time.Millisecond)
    }
}

// =============== ฟังก์ชัน JSON แยก ===============
func toJSONAir(airID int, temp, hum float64, pumpStatus bool) string {
    // type=air , air_id=??
    return fmt.Sprintf(`{"type":"air","air_id":%d,"temp":%.1f,"air_humidity":%.1f,"pump_status":%t}`,
        airID, temp, hum, pumpStatus)
}

func toJSONSoil(soilID int, soil float64, pumpStatus bool) string {
    // type=soil , soil_id=??
    return fmt.Sprintf(`{"type":"soil","soil_id":%d,"soil_humidity":%.1f,"pump_status":%t}`,
        soilID, soil, pumpStatus)
}

// ตรวจ threshold
func changedBeyondThreshold(oldVal, newVal, threshold float64) bool {
    return math.Abs(newVal-oldVal) > threshold
}

// อ่านทีละไบต์จนเจอ '\\n'
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

func clampValue(v int) uint32 {
    if v < 0 {
        return 0
    } else if v > 100 {
        return 100
    }
    return uint32(v)
}