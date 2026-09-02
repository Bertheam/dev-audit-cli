plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.fixture.explicit"
    compileSdk = 35
    ndkVersion = "27.0.12077973"

    defaultConfig {
        minSdk = 23
        targetSdk = 35
    }
}

java {
    toolchain {
        languageVersion = JavaLanguageVersion.of(17)
    }
}
