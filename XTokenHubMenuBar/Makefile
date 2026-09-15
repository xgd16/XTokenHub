APP_NAME  := XTokenHubMenuBar
PROJECT   := $(APP_NAME).xcodeproj
SCHEME    := $(APP_NAME)
BUILD_DIR := .build
# 版本号取自 project.yml 的 MARKETING_VERSION(App 与 Widget 同源),DMG 名自动跟随,不用手工同步
VERSION   := $(shell sed -n 's/^ *MARKETING_VERSION: *"\(.*\)"/\1/p' project.yml | head -1)
ifeq ($(strip $(VERSION)),)
VERSION   := 0.0.0
endif
DMG_NAME  ?= $(APP_NAME)-$(VERSION).dmg

.PHONY: gen build run test icon package dmg install clean

## 生成 Xcode 工程(修改 project.yml 后需要重新执行)
gen:
	xcodegen generate

## 编译 Debug 产物
build: gen
	xcodebuild -project $(PROJECT) -scheme $(SCHEME) -configuration Debug -derivedDataPath $(BUILD_DIR) build

## 编译并启动应用
run: build
	open $(BUILD_DIR)/Build/Products/Debug/$(APP_NAME).app

## 运行单元测试
test: gen
	xcodebuild -project $(PROJECT) -scheme $(SCHEME) -configuration Debug -derivedDataPath $(BUILD_DIR) test

## 重新生成 AppIcon(1024 master + 多尺寸派生)
icon:
	swift Tools/make_icon.swift

## 制作 DMG 安装包(Release 构建 + /Applications 拖拽链接)
## 用法:make package  或  make dmg
## 用 ditto 而非 cp -R 复制:保留包内符号链接与元数据(.app 内含 Widget 扩展)
package: gen
	rm -f $(DMG_NAME)
	xcodebuild -project $(PROJECT) -scheme $(SCHEME) -configuration Release -derivedDataPath $(BUILD_DIR) build
	rm -rf .dmg-stage && mkdir -p .dmg-stage
	ditto $(BUILD_DIR)/Build/Products/Release/$(APP_NAME).app .dmg-stage/$(APP_NAME).app
	ln -s /Applications .dmg-stage/Applications
	# hdiutil create 在新 macOS 上已废弃,改用 diskutil image create from
	diskutil image create from --format UDZO --volumeName $(APP_NAME) .dmg-stage $(DMG_NAME)
	rm -rf .dmg-stage
	@echo "安装包已生成: $(CURDIR)/$(DMG_NAME) ($$(du -h $(DMG_NAME) | cut -f1))"

## 同 package(DMG 安装包)
dmg: package

clean:
	rm -rf $(BUILD_DIR) $(PROJECT) *.dmg
