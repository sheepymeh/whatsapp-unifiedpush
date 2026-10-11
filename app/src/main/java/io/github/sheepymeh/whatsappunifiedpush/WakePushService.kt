package io.github.sheepymeh.whatsappunifiedpush

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.SystemClock
import android.util.Log
import org.unifiedpush.android.connector.FailedReason
import org.unifiedpush.android.connector.PushService
import org.unifiedpush.android.connector.data.PushEndpoint
import org.unifiedpush.android.connector.data.PushMessage

internal object Wake {
    private const val TAG = "wa-up"
    private const val COOLDOWN_MS = 60_000L
    const val ENDPOINT_KEY = "endpoint"
    private val WAKE_MESSAGE = "wake".toByteArray()

    fun isWake(message: PushMessage) = message.content.contentEquals(WAKE_MESSAGE)
    fun prefs(context: Context) = context.getSharedPreferences("shim", Context.MODE_PRIVATE)

    @Synchronized fun send(context: Context) {
        val prefs = prefs(context)
        val now = SystemClock.elapsedRealtime()
        val last = prefs.getLong("lastWake", -1)
        if (last >= 0 && now >= last && now - last < COOLDOWN_MS) return

        try {
            context.sendBroadcast(Intent("com.facebook.rti.fbns.intent.RECEIVE").apply {
                component = ComponentName("com.whatsapp", "com.whatsapp.infra.push.WAFbnsPreloadReceiver")
                addCategory("com.whatsapp")
                addFlags(Intent.FLAG_INCLUDE_STOPPED_PACKAGES)
            })
            prefs.edit().putLong("lastWake", now).commit()
            Log.i(TAG, "Wake broadcast sent")
        } catch (e: RuntimeException) {
            Log.w(TAG, "Wake broadcast failed", e)
        }
    }
}

class WakePushService : PushService() {
    override fun onMessage(message: PushMessage, instance: String) {
        if (Wake.isWake(message)) Wake.send(this)
    }

    override fun onNewEndpoint(endpoint: PushEndpoint, instance: String) {
        val prefs = Wake.prefs(this)
        val previous = prefs.getString(Wake.ENDPOINT_KEY, null)
        prefs.edit().putString(Wake.ENDPOINT_KEY, endpoint.url).apply()
        Registration.registered(this)
        if (previous != endpoint.url) {
            Log.i("wa-up", "UnifiedPush endpoint ${if (previous == null) "received" else "changed"}; check sender configuration")
        }
    }

    override fun onUnregistered(instance: String) {
        Wake.prefs(this).edit().remove(Wake.ENDPOINT_KEY).apply()
        Registration.retry(this)
        Log.w("wa-up", "UnifiedPush unregistered; retry scheduled")
    }

    override fun onRegistrationFailed(reason: FailedReason, instance: String) {
        Registration.retry(this, Registration.FAILURE_RETRY_MS)
        Log.w("wa-up", "Registration failed: $reason")
    }
}
