APP_NAME = kuaima_cli.exe
APP_PKG = ./cmd/kuaima_cli
BUILD_OUTPUT = dist/$(APP_NAME)
VERSION ?= 0.2.0
COMMON_GOFLAGS = -buildvcs=false
RELEASE_GOFLAGS = -trimpath -ldflags "-s -w -X kuaima_cli/internal/app.AppVersion=$(VERSION)"

ifeq ($(OS),Windows_NT)
BUILD_SKILL_CMD = powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/build_skill.ps1
else
BUILD_SKILL_CMD = bash scripts/build_skill.sh
endif


build:
	go build $(COMMON_GOFLAGS) -o $(BUILD_OUTPUT) $(APP_PKG)

release:
	go build $(COMMON_GOFLAGS) $(RELEASE_GOFLAGS) -o $(BUILD_OUTPUT) $(APP_PKG)

build_skill:
	$(BUILD_SKILL_CMD)

run:
	dist/kuaima_cli.exe

run_web:
	dist/kuaima_cli.exe webui -log log.log

test:
	dist/kuaima_cli.exe -stream false 你是什么模型

test_stream:
	dist/kuaima_cli.exe 给我背诵一下滕王阁序

#查询余额
balance:
	dist/kuaima_cli.exe balance

#充值
recharge:
	dist/kuaima_cli.exe recharge

#加上-stream参数，可以避免1min超时错误 
#一般生图时间 1k约80s， 2k约120s， 4k约180s
test_img:
	dist/kuaima_cli.exe -log log.log -image-generation -file https://weixin.uctphp.com/static/images/ailogo.png 帮我画张快马AI cli的宣传海报，便捷的生图工具，支持gpt-image-2等最新模型,可接入各种agent智能体，参考图是logo

#指定图片大小 4k
#比例   1:1      2:3         3:2
#1k: 1024x1024 1024x1536 1536x1024
#2k: 2048x2048 1440x2560 2560x1440 
#4k: 2880x2880 2160x3840 3840x2160
test_img2:
	dist/kuaima_cli.exe -log log.log -image-size 1024x1024 -image-generation -file https://weixin.uctphp.com/static/images/ailogo.png 帮我画张快马AI cli的宣传海报，便捷的生图工具，支持gpt-image-2等最新模型,可接入各种agent智能体，参考图是快马AI的logo

#生成4k图 容易失败
test_4k:
	dist/kuaima_cli.exe -log log.log -image-size 2160x3840 -image-generation -file https://weixin.uctphp.com/static/images/ailogo.png 帮我画张快马AI cli的宣传海报，便捷的生图工具，支持gpt-image-2等最新模型,可接入各种agent智能体，参考图是快马AI的logo

#指定生图模型 数量
# gpt-image-2 gpt-image-1.5 gpt-image-1 dalle-e-3 dalle-e-2
test_img3:
	dist/kuaima_cli.exe -log log.log -image-model gpt-image-1.5 -image-count 2 -image-size 1024x1024 -image-generation -file https://weixin.uctphp.com/static/images/ailogo.png 帮我画张快马AI cli的宣传海报，便捷的生图工具，支持gpt-image-2等最新模型,可接入各种agent智能体，参考图是快马AI的logo

#这个是否成功率高一些 ?
test_img_s:
	dist/kuaima_cli.exe -log log.log -system "Just generate the image; do not return a prompt or SVG" -image-model gpt-image-2 -image-generation -file https://weixin.uctphp.com/static/images/ailogo.png 帮我画张快马AI cli的宣传海报，便捷的生图工具，支持gpt-image-2等最新模型,可接入各种agent智能体，参考图是快马AI的logo

#改图
test_img_i:
	dist/kuaima_cli.exe -log log.log -file response-image-01-03.png -image-generation 帮我把里面的示例图片改成实际场景，比如电商主图，宣传海报，ppt等

#需要后端支持 ?
test_gen:
	dist/kuaima_cli.exe -log log.log -file response-image-01-03.png image 帮我把里面的示例图片改成实际场景，比如电商主图，宣传海报，ppt等
