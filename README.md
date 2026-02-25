# Structure Project 

pwa-gis-tracking/
├── main.go                    # Entry point - port 5011
├── go.mod                     # Go modules
├── .env                       # PostgreSQL + MongoDB config
├── config/
│   └── database.go            # DB connections (PG + Mongo)
├── models/
│   └── office.go              # Data models
├── services/
│   ├── office_service.go      # PG queries → pwa_office234
│   └── mongo_service.go       # MongoDB counting, date filter, export
├── handlers/
│   └── api.go
    └── auth.go                # API handlers (dashboard, export xlsx/geojson)
├── routes/
│   └── routes.go              # Route registration
├── templates/
│   ├── dashboard.html         # หน้า 1: ภาพรวมตามเขต + pie charts
│   └── detail.html            # หน้า 2: รายละเอียดสาขา + export
    └── login.html 
static/
├── icons/          ← วาง SVG จาก outputs/static/icons/
│   ├── Valve.svg
│   ├── FireHydrant.svg
│   ├── Meter.svg
│   ├── BLDG.svg
│   ├── Leakpoint.svg
│   └── PWASmall.svg
├── images/         ← วาง logo จาก outputs/static/images/
│   └── pwa-logo.jpg
├── css/
│   └── style.css
└── js/
    ├── dashboard.js
    ├── detail.js
    └── thai_custom_date.js
    ├── css/style.css           # Custom dark theme (IBM Plex Sans Thai)
    └── js/
        ├── dashboard.js        # Dashboard logic + Chart.js
        └── detail.js           # Detail + export logic
        └── thai_custom_date.js # custom A.D. -> B.E. logic