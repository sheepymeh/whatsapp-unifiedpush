package io.github.sheepymeh.whatsappunifiedpush

import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.SharedPreferences
import android.os.Build
import android.os.Bundle
import android.os.PersistableBundle
import android.widget.Button
import android.widget.TextView
import android.widget.Toast
import org.unifiedpush.android.connector.UnifiedPush

class MainActivity : Activity() {
    private lateinit var endpointView: TextView
    private lateinit var statusView: TextView
    private lateinit var copyButton: Button

    private val preferences by lazy { Wake.prefs(this) }
    private val preferenceListener = SharedPreferences.OnSharedPreferenceChangeListener { _, key ->
        if (key == Wake.ENDPOINT_KEY) runOnUiThread { showEndpoint() }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        endpointView = findViewById(R.id.endpoint)
        statusView = findViewById(R.id.status)
        copyButton = findViewById(R.id.copy)

        findViewById<Button>(R.id.register).setOnClickListener { requestRegistration() }
        copyButton.setOnClickListener {
            val endpoint = preferences.getString(Wake.ENDPOINT_KEY, null) ?: return@setOnClickListener
            val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
            val clip = ClipData.newPlainText("UnifiedPush endpoint", endpoint)
            clip.description.extras = PersistableBundle().apply {
                putBoolean("android.content.extra.IS_SENSITIVE", true)
            }
            clipboard.setPrimaryClip(clip)
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) {
                Toast.makeText(this, "Endpoint copied", Toast.LENGTH_SHORT).show()
            }
        }

        Registration.schedule(this)
        requestRegistration()
        showEndpoint()
    }

    override fun onStart() {
        super.onStart()
        preferences.registerOnSharedPreferenceChangeListener(preferenceListener)
        showEndpoint()
    }

    override fun onStop() {
        preferences.unregisterOnSharedPreferenceChangeListener(preferenceListener)
        super.onStop()
    }

    private fun requestRegistration() {
        if (Registration.register(this)) {
            showEndpoint()
            return
        }
        // No saved distributor (or the old one was removed): let Android choose.
        try {
            UnifiedPush.tryUseCurrentOrDefaultDistributor(this) { selected ->
                if (selected && Registration.register(this)) {
                    Toast.makeText(this, "Registration requested", Toast.LENGTH_SHORT).show()
                } else {
                    Toast.makeText(this, "Select an available UnifiedPush distributor", Toast.LENGTH_LONG).show()
                }
                showEndpoint()
            }
        } catch (e: RuntimeException) {
            Toast.makeText(this, "Could not select a push distributor", Toast.LENGTH_LONG).show()
        }
    }

    private fun showEndpoint() {
        val endpoint = preferences.getString(Wake.ENDPOINT_KEY, null)
        endpointView.text = endpoint ?: "Waiting for a UnifiedPush endpoint..."
        copyButton.isEnabled = endpoint != null
        val distributor = runCatching { UnifiedPush.getSavedDistributor(this) }.getOrNull()
        val available = runCatching {
            distributor != null && distributor in UnifiedPush.getDistributors(this)
        }.getOrDefault(false)
        statusView.text = when {
            distributor == null -> "Choose a push distributor"
            !available -> "Distributor unavailable"
            endpoint == null -> "Waiting for registration"
            else -> "Endpoint saved"
        }
    }
}
