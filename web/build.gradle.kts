plugins {
    kotlin("multiplatform") version "2.2.20"
    kotlin("plugin.serialization") version "2.2.20"
}

kotlin {
    js {
        browser {
            commonWebpackConfig {
                outputFileName = "app.js"
            }
        }
        binaries.executable()
    }

    sourceSets {
        jsMain.dependencies {
            implementation("org.jetbrains.kotlinx:kotlinx-coroutines-core:1.10.2")
            implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.9.0")
            implementation("org.jetbrains.kotlinx:kotlinx-html:0.12.0")
        }
    }
}
