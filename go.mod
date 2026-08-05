module kuaima_cli

go 1.25

require (
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/yalue/onnxruntime_go v1.32.0
)

replace github.com/yalue/onnxruntime_go => ./third_party/onnxruntime_go
