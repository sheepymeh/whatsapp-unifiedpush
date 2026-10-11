package io.github.sheepymeh.whatsappunifiedpush

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.BroadcastReceiver
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.util.Log
import org.unifiedpush.android.connector.UnifiedPush

internal object Registration {
    private const val TAG = "wa-up"
    private const val DAILY = 42001
    private const val RETRY = 42002
    private const val DAY_MS = 24 * 60 * 60 * 1000L
    private const val RETRY_MS = 60_000L
    const val FAILURE_RETRY_MS = 15 * 60_000L

    private fun scheduler(context: Context) = context.getSystemService(JobScheduler::class.java)
    private fun component(context: Context) = ComponentName(context, RegistrationJob::class.java)

    fun start(context: Context) {
        schedule(context)
        register(context)
    }

    fun schedule(context: Context) {
        val jobs = scheduler(context)
        if (jobs.getPendingJob(DAILY) == null) {
            jobs.schedule(JobInfo.Builder(DAILY, component(context))
                .setPeriodic(DAY_MS)
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setPersisted(true)
                .build())
        }
    }

    fun register(context: Context): Boolean {
        try {
            val saved = UnifiedPush.getSavedDistributor(context)
            if (saved == null || saved !in UnifiedPush.getDistributors(context)) {
                Log.w(TAG, "No available UnifiedPush distributor selected")
                return false
            }
            UnifiedPush.register(context)
            Log.i(TAG, "UnifiedPush registration requested")
            return true
        } catch (e: RuntimeException) {
            Log.w(TAG, "UnifiedPush registration request failed", e)
            return false
        }
    }

    fun retry(context: Context, delayMs: Long = RETRY_MS) {
        val jobs = scheduler(context)
        if (jobs.getPendingJob(RETRY) == null) {
            jobs.schedule(JobInfo.Builder(RETRY, component(context))
                .setMinimumLatency(delayMs)
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .build())
        }
    }

    fun registered(context: Context) = scheduler(context).cancel(RETRY)
}

class RegistrationJob : JobService() {
    override fun onStartJob(params: JobParameters): Boolean {
        Registration.register(this)
        return false
    }

    override fun onStopJob(params: JobParameters): Boolean = false
}

class StartReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == Intent.ACTION_BOOT_COMPLETED ||
            intent.action == Intent.ACTION_MY_PACKAGE_REPLACED) {
            Registration.start(context)
        }
    }
}
