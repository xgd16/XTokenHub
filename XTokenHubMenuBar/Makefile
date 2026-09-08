APP_NAME  := XTokenHubMenuBar
PROJECT   := $(APP_NAME).xcodeproj
SCHEME    := $(APP_NAME)
BUILD_DIR := .build
DMG_NAME  ?= $(APP_NAME)-1.0.0.dmg

.PHONY: gen build run test icon package clean

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
package: gen
	xcodebuild -project $(PROJECT) -scheme $(SCHEME) -configuration Release -derivedDataPath $(BUILD_DIR) build
	rm -rf .dmg-stage && mkdir -p .dmg-stage
	cp -R $(BUILD_DIR)/Build/Products/Release/$(APP_NAME).app .dmg-stage/
	ln -s /Applications .dmg-stage/Applications
	hdiutil create -volname $(APP_NAME) -srcfolder .dmg-stage -ov -format UDZO $(DMG_NAME)
	rm -rf .dmg-stage

clean:
	rm -rf $(BUILD_DIR) $(PROJECT) *.dmg
