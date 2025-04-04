🌱 Smart Farm

An IoT-based Smart Farming System that provides real-time monitoring and control of environmental conditions, integrated with a user-friendly Dashboard and Desktop GUI.

⸻

🖥️ Overview

Smart Farm is an intelligent farming system utilizing IoT technology. It uses sensors to collect environmental data such as Air Temperature, Air Humidity, and Soil Moisture, and sends this data through a microcontroller (Raspberry Pi Pico) to a server.

The collected data is stored in a PostgreSQL database, displayed on a real-time web-based dashboard, and can be controlled through an intuitive desktop GUI application for operating equipment such as water pumps and LEDs.

⸻

📡 Hardware Components
	•	Microcontroller:
	•	Raspberry Pi Pico (Maker Pi Pico Rev 1.2)
	•	Sensors:
	•	DHT22 sensor (Temperature and Air Humidity)
	•	Capacitive Soil Moisture sensor (Soil Moisture)
	•	Actuators:
	•	Water Pump
	•	LED lighting (GPIO 13, GPIO 14, GPIO 15)

⸻
🛠️ Technologies & Frameworks

Component
Description
Programming -> Go (server-side), TinyGo (Microcontroller)
Web Dashboard -> HTML, CSS, JavaScript (Chart.js)
Desktop GUI -> Fyne (Go GUI framework)
Database ->PostgreSQL
Communication -> MQTT Protocol, Serial Communication
Web Server -> Gorilla Mux (Go Router)

⸻

📝 Notes & Issues

If you encounter issues or have questions, feel free to create an issue on GitHub or reach out via provided contact details in the repository.

⸻
📖 License

© 2024 Smart Farm Project
Licensed under the MIT License.

⸻

