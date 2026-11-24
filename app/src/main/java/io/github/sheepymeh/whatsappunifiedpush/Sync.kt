package io.github.sheepymeh.whatsappunifiedpush

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import android.net.Uri
import android.util.Log
import java.net.HttpURLConnection
import java.net.URL

/** Uploads the push subscription to the server. */
internal object Sync {
    private const val JOB = 42003
    private const val RETRY_MS = 60_000L
    private const val TIMEOUT_MS = 15_000
    private const val NO_RESPONSE = -1
    const val URL_KEY = "serverUrl"
    const val TOKEN_KEY = "serverToken"
    const val STATUS_KEY = "syncStatus"
    private const val SYNCED_KEY = "synced"

    fun validUrl(url: String) = Uri.parse(url).let { it.scheme == "https" && !it.host.isNullOrEmpty() }

    /** [force] uploads even if already synced. */
    fun schedule(context: Context, force: Boolean = false) {
        if (force) Wake.prefs(context).edit().remove(SYNCED_KEY).apply()
        val result = context.getSystemService(JobScheduler::class.java).schedule(
            JobInfo.Builder(JOB, ComponentName(context, SyncJob::class.java))
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setBackoffCriteria(RETRY_MS, JobInfo.BACKOFF_POLICY_EXPONENTIAL)
                .setPersisted(true)
                .build())
        if (result != JobScheduler.RESULT_SUCCESS) {
            Log.w(TAG, "Could not schedule sync job")
            setStatus(context, "Could not schedule the upload")
        }
    }

    private fun setStatus(context: Context, text: String) =
        Wake.prefs(context).edit().putString(STATUS_KEY, text).apply()

    /** Returns whether to retry. */
    fun upload(context: Context): Boolean {
        val prefs = Wake.prefs(context)
        val subscription = prefs.getString(Wake.ENDPOINT_KEY, null)
        val url = prefs.getString(URL_KEY, null)
        val token = prefs.getString(TOKEN_KEY, null)
        Log.i(TAG, "Sync job started")
        if (subscription == null) return false.also { setStatus(context, "Waiting for the push subscription") }
        if (url == null || token == null) return false // not paired yet
        if (!validUrl(url)) return false.also { setStatus(context, "Invalid server URL") }
        val state = "$url\n$token\n$subscription"
        if (prefs.getString(SYNCED_KEY, null) == state) return false

        var error: String? = null
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
        } catch (e: Exception) {
            Log.w(TAG, "Subscription upload failed", e)
            error = e.javaClass.simpleName
            NO_RESPONSE
        }
        val ok = code in 200..299
        prefs.edit().apply {
            if (ok) putString(SYNCED_KEY, state)
            putString(STATUS_KEY, when {
                ok -> "Subscription synced to server"
                code == 401 -> "Server rejected the token"
                code in 400..499 -> "Server rejected the subscription (HTTP $code)"
                code == NO_RESPONSE -> "Upload failed ($error); will retry"
                else -> "Server error (HTTP $code); will retry"
            })
        }.apply()
        Log.i(TAG, "Subscription upload returned $code")
        return code == NO_RESPONSE || code >= 500
    }
}

class SyncJob : JobService() {
    override fun onStartJob(params: JobParameters): Boolean {
        Thread { jobFinished(params, Sync.upload(this)) }.start()
        return true
    }

    override fun onStopJob(params: JobParameters): Boolean = true
}
