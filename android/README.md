# 📱 Android APK Build Guide

## Требования

Для сборки Android APK необходимо установить:

1. **Android SDK** (Android Studio или command-line tools)
2. **Go 1.21+**
3. **gomobile**

## Установка

### 1. Установи Android SDK

**Вариант A: Android Studio (рекомендуется)**
- Скачай: https://developer.android.com/studio
- Установи
- Открой SDK Manager и установи:
  - Android SDK Platform (API 21+)
  - Android SDK Build-Tools
  - Android NDK

**Вариант B: Command-line tools только**
```bash
# Скачай command-line tools
# https://developer.android.com/studio#command-tools

# Распакуй и настрой
export ANDROID_HOME=$HOME/android-sdk
export PATH=$PATH:$ANDROID_HOME/tools:$ANDROID_HOME/platform-tools
```

### 2. Установи gomobile

```bash
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
```

## Сборка APK

### Вариант 1: AAR библиотека (для интеграции в Android app)

```bash
cd mobile
gomobile bind -target android -o tgwsproxy.aar ./mobile
```

Получишь `tgwsproxy.aar` — библиотека для подключения к Android проекту.

### Вариант 2: Полное APK приложение

Для создания полноценного APK нужен Android проект с UI.

**Структура Android проекта:**
```
android/
├── app/
│   ├── src/main/java/.../MainActivity.java
│   ├── src/main/AndroidManifest.xml
│   └── build.gradle
├── build.gradle
├── settings.gradle
└── tgwsproxy.aar  (из шага выше)
```

**Пример build.gradle:**
```gradle
plugins {
    id 'com.android.application'
}

android {
    compileSdk 34
    defaultConfig {
        applicationId "com.github.yosyatarbeep.tgwsproxy"
        minSdk 21
        targetSdk 34
        versionCode 1
        versionName "1.0"
    }
}

dependencies {
    implementation files('libs/tgwsproxy.aar')
}
```

**Сборка APK:**
```bash
cd android
./gradlew assembleDebug
# APK будет в: app/build/outputs/apk/debug/app-debug.apk
```

## Быстрая сборка (если есть Android SDK)

```bash
# В корне проекта
make android

# Или вручную
gomobile bind -target android -o android/tgwsproxy.aar ./mobile
cd android && ./gradlew assembleDebug
```

## Установка на устройство

```bash
adb install app/build/outputs/apk/debug/app-debug.apk
```

---

## 📝 Заметки

- **Min SDK:** Android 5.0 (API 21)
- **Target SDK:** Android 14 (API 34)
- **Архитектуры:** arm64-v8a, armeabi-v7a, x86_64
- **Размер APK:** ~10-15 MB (включая Go runtime)

## 🔧 Troubleshooting

### "Android SDK not found"
```bash
# Укажи путь к SDK
export ANDROID_HOME=/path/to/android-sdk
export PATH=$PATH:$ANDROID_HOME/tools:$ANDROID_HOME/platform-tools
```

### "NDK not found"
```bash
# Установи NDK через SDK Manager
# Или задай путь
export ANDROID_NDK_HOME=$ANDROID_HOME/ndk/<version>
```

---

## 📦 Готовые сборки

Смотри Releases: https://github.com/y0sy4/tg-ws-proxy-go/releases
