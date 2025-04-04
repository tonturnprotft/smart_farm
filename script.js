function updateDateTime() {
    const now = new Date();
    const days = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
    const day = days[now.getDay()];
    const hours = now.getHours().toString().padStart(2, '0');
    const minutes = now.getMinutes().toString().padStart(2, '0');
    document.getElementById("datetime").innerText = `${day} : ${hours}:${minutes}`;
  }
  
  function updateDayCount() {
    const dayCount = localStorage.getItem("dayCount") || 0;
    document.getElementById("day-count").innerText = dayCount;
  }
  
  function startDayCount() {
    localStorage.setItem("dayCount", 1);
    updateDayCount();
    alert("Day count started successfully!");
  }
  
  function resetDayCount() {
    localStorage.setItem("dayCount", 0);
    updateDayCount();
  }
  
  // สร้าง doughnut chart แสดงเปอร์เซ็นต์ LED
  function createLEDChart(chartId, value, color) {
    const canvas = document.getElementById(chartId);
    if (!canvas) return null;
    const ctx = canvas.getContext('2d');
  
    return new Chart(ctx, {
      type: 'doughnut',
      data: {
        labels: ['ON', 'OFF'],
        datasets: [{
          data: [value, 100 - value],
          backgroundColor: [color, '#444'],
          borderWidth: 1
        }]
      },
      options: {
        responsive: true,
        maintainAspectRatio: true,
        aspectRatio: 1,
        plugins: {
          legend: { display: false },
          tooltip: { enabled: false }
        }
      }
    });
  }
  
  // ช่วยอัปเดตค่า LED
  function updateLEDChart(chart, chartPercentID, brightness) {
    if (!chart) return;
    chart.data.datasets[0].data = [brightness, 100 - brightness];
    chart.update();
    if (chartPercentID) {
      document.getElementById(chartPercentID).innerText = `${brightness}%`;
    }
  }
  
  // สร้างกราฟ LED ทั้งสาม
  let ledChart1, ledChart2, ledChart3;
  
  // moisture chart
  const ctx = document.getElementById('moistureChart').getContext('2d');
  const moistureChart = new Chart(ctx, {
    type: 'line',
    data: {
      labels: [],
      datasets: [{
        label: 'Soil Moisture',
        data: [],
        borderColor: '#4CAF50',
        backgroundColor: 'rgba(76, 175, 80, 0.2)',
        fill: true
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      scales: {
        x: {
          title: { display: true, text: 'Time', color: 'white' },
          ticks: { color: 'white' }
        },
        y: {
          title: { display: true, text: 'Moisture (%)', color: 'white' },
          beginAtZero: true,
          ticks: { color: 'white' }
        }
      }
    }
  });
  
  function updateGraph() {
    const selectedValue = document.getElementById("timeRange").value;
    // TODO: ควรดึงข้อมูลย้อนหลังจาก server เช่น /soil-history?range=1
    console.log(`อัปเดตกราฟ: แสดงข้อมูล ${selectedValue} นาทีล่าสุด`);
  }
  
  // ฟังก์ชันหลัก: ดึงค่าจาก /sensor-data
  function updateSensorData() {
    fetch("/sensor-data")
      .then(res => {
        if (!res.ok) {
          throw new Error(`HTTP error! status: ${res.status}`);
        }
        return res.json();
      })
      .then(data => {
        // อัปเดตอุณหภูมิ/ความชื้น air1
        if (data.air1_temp !== undefined) {
          document.getElementById("temp1-value").innerText = data.air1_temp.toFixed(1) + "°";
        }
        if (data.air1_humidity !== undefined) {
          document.getElementById("humidity1-value").innerText = data.air1_humidity.toFixed(1) + "%";
        }
  
        // air2
        if (data.air2_temp !== undefined) {
          document.getElementById("temp2-value").innerText = data.air2_temp.toFixed(1) + "°";
        }
        if (data.air2_humidity !== undefined) {
          document.getElementById("humidity2-value").innerText = data.air2_humidity.toFixed(1) + "%";
        }
  
        // Soil moisture
        if (data.soil_humidity !== undefined) {
          document.getElementById("moisture-value").innerText = data.soil_humidity.toFixed(1) + "%";
        }
  
        // ปั๊มน้ำ
        if (typeof data.pump_status === 'boolean') {
          const pumpToggle = document.getElementById("pump-toggle");
          const pumpStatus = document.getElementById("pump-status");
          if (data.pump_status) {
            pumpToggle.classList.add("on");
            pumpStatus.innerText = "ON";
          } else {
            pumpToggle.classList.remove("on");
            pumpStatus.innerText = "OFF";
          }
        }
  
        // LED
        if (data.led1 !== undefined) {
          updateLEDChart(ledChart1, "led1-percent", data.led1);
        }
        if (data.led2 !== undefined) {
          updateLEDChart(ledChart2, "led2-percent", data.led2);
        }
        if (data.led3 !== undefined) {
          updateLEDChart(ledChart3, "led3-percent", data.led3);
        }
      })
      .catch(err => {
        console.error("Error fetching sensor data:", err);
      });
  }
  
  // เริ่มต้น
  document.addEventListener("DOMContentLoaded", function() {
    // สร้าง chart LED
    ledChart1 = createLEDChart('ledChart1', 75, '#ff0000');
    ledChart2 = createLEDChart('ledChart2', 50, '#00ff00');
    ledChart3 = createLEDChart('ledChart3', 90, '#0000ff');
  
    updateDayCount();
    setInterval(updateDateTime, 1000);
  
    // เรียกทุก 2 วิ
    setInterval(updateSensorData, 2000);
    updateSensorData();
  });
