import java.util.Properties

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// Release key: android/key.properties locally, VPSADMIN_* env vars on CI (see
// .github/workflows/release.yml). Every APK served for self-update must be signed with
// it, or Android refuses the update.
val keyProps = Properties().apply {
    rootProject.file("key.properties").takeIf { it.exists() }?.inputStream()?.use { load(it) }
}
fun signing(name: String, env: String): String? = System.getenv(env) ?: keyProps.getProperty(name)

android {
    namespace = "fr.beedy.vps_admin"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "fr.beedy.vps_admin"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = maxOf(flutter.minSdkVersion, 23) // encrypted secure storage
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    signingConfigs {
        create("release") {
            signing("storeFile", "VPSADMIN_KEYSTORE")?.let { storeFile = file(it) }
            storePassword = signing("storePassword", "VPSADMIN_KEYSTORE_PASSWORD")
            keyAlias = signing("keyAlias", "VPSADMIN_KEY_ALIAS")
            keyPassword = signing("keyPassword", "VPSADMIN_KEY_PASSWORD")
        }
    }

    buildTypes {
        release {
            val release = signingConfigs.getByName("release")
            // Without the release key, fall back to debug keys so `flutter run --release` still works.
            signingConfig = if (release.storeFile != null) release else signingConfigs.getByName("debug")
            // No R8: plugins generated with jnigen (shared_preferences, path_provider) call
            // their Kotlin side by name through JNI, and R8 strips what it cannot see used,
            // which crashed the app at startup in release builds. The Java part is tiny.
            isMinifyEnabled = false
            isShrinkResources = false
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}

dependencies {
    implementation("androidx.core:core:1.13.1") // FileProvider for the self-update installer
}
