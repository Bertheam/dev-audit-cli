plugins {
    id("com.android.application")
}

android {
    namespace = "dev.fixture.dynamic"
    compileSdk = providers.gradleProperty("compileSdk").get().toInt()
    ndkVersion = System.getenv("FIXTURE_NDK_VERSION")
}
