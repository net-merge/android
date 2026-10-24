import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

android {
    namespace = "org.netmerge.android"
    compileSdk = 36

    defaultConfig {
        applicationId = "org.netmerge.android"
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"
    }

    // Releases are signed with the net-merge key when CI has it (ANDROID_KEYSTORE, from the repo's
    // secrets); every other build (pull requests, local) uses the debug key.
    val releaseKey = System.getenv("ANDROID_KEYSTORE")
    signingConfigs {
        if (releaseKey != null) {
            create("release") {
                storeFile = file(releaseKey)
                storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("ANDROID_KEY_ALIAS")
                keyPassword = System.getenv("ANDROID_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            // Phones only (no emulator x86 code), which keeps the download about a third smaller.
            ndk {
                abiFilters += listOf("arm64-v8a", "armeabi-v7a")
            }
            signingConfig = signingConfigs.getByName(if (releaseKey != null) "release" else "debug")
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}

dependencies {
    // Built by scripts/build-go.sh (gomobile bind of golib/netmerge).
    implementation(files("libs/netmerge.aar"))

    implementation(platform("androidx.compose:compose-bom:2025.06.00"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.compose.animation:animation")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.activity:activity-compose:1.10.1")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")
}
