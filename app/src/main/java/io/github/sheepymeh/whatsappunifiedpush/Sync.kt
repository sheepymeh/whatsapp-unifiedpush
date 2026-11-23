package io.github.sheepymeh.whatsappunifiedpush

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import android.net.Uri
import android.util.Log
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/** Uploads the push subscription to the server. */
internal object Sync {
    private const val TAG = "wa-up"
    private const val JOB = 42003
    private const val RETRY_MS = 60_000L
    private const val TIMEOUT_MS = 15_000
    const val URL_KEY = "serverUrl"
    const val TOKEN_KEY = "serverToken"
    const val STATUS_KEY = "syncStatus"
    private const val SYNCED_KEY = "synced"

    fun validUrl(url: String) = Uri.parse(url).let { it.scheme == "https" && !it.host.isNullOrEmpty() }

    /** [force] uploads even if already synced. */
    fun schedule(context: Context, force: Boolean = false) {
        if (force) Wake.prefs(context).edit().remove(SYNCED_KEY).apply()
        context.getSystemService(JobScheduler::class.java).schedule(
            JobInfo.Builder(JOB, ComponentName(context, SyncJob::class.java))
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setBackoffCriteria(RETRY_MS, JobInfo.BACKOFF_POLICY_EXPONENTIAL)
                .setPersisted(true)
                .build())
    }

    /** Returns whether to retry. */
    fun upload(context: Context): Boolean {
        val prefs = Wake.prefs(context)
        val subscription = prefs.getString(Wake.ENDPOINT_KEY, null)
        val url = prefs.getString(URL_KEY, null)
        val token = prefs.getString(TOKEN_KEY, null)
        if (subscription == null || url == null || token == null || !validUrl(url)) return false
        val state = "$url\n$token\n$subscription"
        if (prefs.getString(SYNCED_KEY, null) == state) return false

        val code = try {
            (URL(url).openConnection() as HttpURLConnection).run {
                try {
                    requestMethod = "POST"
                    instanceFollowRedirects = false
                    connectTimeout = TIMEOUT_MS
                    readTimeout = TIMEOUT_MS
                    doOutput = true
                    setRequestProperty("Authorization", "Bearer $token")
                    setRequestProperty("Content-Type", "application/json")
                    outputStream.use { it.write(subscription.toByteArray()) }
                    responseCode
                } finally {
                    disconnect()
                }
            }
        } catch (e: IOException) {
            -1
        }
        val ok = code in 200..299
        prefs.edit().apply {
            if (ok) putString(SYNCED_KEY, state)
            putString(STATUS_KEY, when {
                ok -> "Subscription synced to server"
                code == 401 -> "Server rejected the token"
                code in 400..499 -> "Server rejected the subscription (HTTP $code)"
                code == -1 -> "Server unreachable; will retry"
                else -> "Server error (HTTP $code); will retry"
            })
        }.apply()
        Log.i(TAG, "Subscription upload returned $code")
        return code == -1 || code >= 500
    }
}

class SyncJob : JobService() {
    override fun onStartJob(params: JobParameters): Boolean {
        Thread { jobFinished(params, Sync.upload(this)) }.start()
        return true
    }

    override fun onStopJob(params: JobParameters): Boolean = true
}
