import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "io.github.senseiredsmoke.netmerge"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.github.senseiredsmoke.netmerge"
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            // Signed with the debug key so CI can produce an installable release APK.
            // Replace with your own signing config before publishing anywhere.
            signingConfig = signingConfigs.getByName("debug")
        }
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
}
