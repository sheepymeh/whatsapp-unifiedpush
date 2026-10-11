plugins {
    id("com.android.application") version "9.4.1"
}

val releaseKeystore = System.getenv("ANDROID_KEYSTORE_FILE")

android {
    namespace = "io.github.sheepymeh.whatsappunifiedpush"
    compileSdk = 35

    defaultConfig {
        applicationId = "io.github.sheepymeh.whatsappunifiedpush"
        minSdk = 26
        targetSdk = 35
        versionCode = System.getenv("RELEASE_VERSION_CODE")?.toIntOrNull() ?: 1
        versionName = System.getenv("RELEASE_VERSION") ?: "0.1.0"
    }

    signingConfigs {
        if (!releaseKeystore.isNullOrBlank()) {
            create("release") {
                storeFile = file(releaseKeystore)
                storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("ANDROID_KEY_ALIAS")
                keyPassword = System.getenv("ANDROID_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        getByName("release") {
            if (!releaseKeystore.isNullOrBlank()) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation("org.unifiedpush.android:connector:3.3.5")
}
