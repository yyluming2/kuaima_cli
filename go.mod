module kuaima_cli

go 1.25

require (
	github.com/CoolBanHub/genppt v0.0.0
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/yalue/onnxruntime_go v1.32.0
)

require golang.org/x/net v0.41.0 // indirect

replace github.com/yalue/onnxruntime_go => ./third_party/onnxruntime_go

replace github.com/CoolBanHub/genppt => ./third_party/genppt
