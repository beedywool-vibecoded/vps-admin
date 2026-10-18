package fr.beedy.vps_admin

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.File

// Self-update: the Dart side downloads the APK through the paired server, this hands it
// to the system installer. Android only accepts it if it is signed with the same key.
class MainActivity : FlutterActivity() {
    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "fr.beedy.vps_admin/updater").setMethodCallHandler { call, result ->
            when (call.method) {
                "version" -> {
                    val info = packageManager.getPackageInfo(packageName, 0)
                    @Suppress("DEPRECATION")
                    val code = if (Build.VERSION.SDK_INT >= 28) info.longVersionCode else info.versionCode.toLong()
                    result.success(mapOf("code" to code, "name" to info.versionName))
                }
                "canInstall" -> result.success(canInstall())
                "openInstallSettings" -> {
                    startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:$packageName")))
                    result.success(null)
                }
                "install" -> {
                    val file = File(call.argument<String>("path")!!)
                    val uri = FileProvider.getUriForFile(this, "$packageName.updates", file)
                    startActivity(
                        Intent(Intent.ACTION_VIEW)
                            .setDataAndType(uri, "application/vnd.android.package-archive")
                            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK),
                    )
                    result.success(null)
                }
                else -> result.notImplemented()
            }
        }
    }

    private fun canInstall() = Build.VERSION.SDK_INT < 26 || packageManager.canRequestPackageInstalls()
}
