module kuaima_cli

go 1.25

require (
	github.com/CoolBanHub/genppt v0.0.0
	github.com/ledongthuc/pdf v0.0.0-20260907135840-6c8c28e0e8a0
	github.com/richardlehane/mscfb v1.0.9
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/refraction-networking/utls v1.6.7-chrome-133-support
	github.com/yalue/onnxruntime_go v1.32.0
	golang.org/x/image v0.32.0
	golang.org/x/text v0.30.0
)

require (
	github.com/andybalholm/brotli v1.0.6 // indirect
	github.com/cloudflare/circl v1.5.0 // indirect
	github.com/klauspost/compress v1.17.4 // indirect
	github.com/richardlehane/msoleps v1.0.3 // indirect
	golang.org/x/crypto v0.39.0 // indirect
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
)

replace github.com/yalue/onnxruntime_go => ./third_party/onnxruntime_go

replace github.com/CoolBanHub/genppt => ./third_party/genppt
