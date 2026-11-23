package io.github.sheepymeh.whatsappunifiedpush

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.content.SharedPreferences
import android.net.Uri
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import org.unifiedpush.android.connector.UnifiedPush

class MainActivity : Activity() {
    private lateinit var statusView: TextView
    private lateinit var syncView: TextView
    private lateinit var urlInput: EditText
    private lateinit var tokenInput: EditText

    private val preferences by lazy { Wake.prefs(this) }
    private val preferenceListener = SharedPreferences.OnSharedPreferenceChangeListener { _, key ->
        if (key == Wake.ENDPOINT_KEY || key == Sync.STATUS_KEY) runOnUiThread { showStatus() }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        statusView = findViewById(R.id.status)
        syncView = findViewById(R.id.sync)
        urlInput = findViewById(R.id.server_url)
        tokenInput = findViewById(R.id.server_token)
        urlInput.setText(preferences.getString(Sync.URL_KEY, ""))
        tokenInput.setText(preferences.getString(Sync.TOKEN_KEY, ""))

        findViewById<Button>(R.id.register).setOnClickListener { requestRegistration() }
        findViewById<Button>(R.id.save).setOnClickListener {
            saveServer(urlInput.text.toString().trim(), tokenInput.text.toString().trim())
        }

        Registration.schedule(this)
        requestRegistration()
        handlePairing(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handlePairing(intent)
    }

    override fun onStart() {
        super.onStart()
        preferences.registerOnSharedPreferenceChangeListener(preferenceListener)
        showStatus()
    }

    override fun onStop() {
        preferences.unregisterOnSharedPreferenceChangeListener(preferenceListener)
        super.onStop()
    }

    /** Handles the wa-up://pair link from the server's QR code. */
    private fun handlePairing(intent: Intent) {
        val data = intent.data?.takeIf { it.scheme == "wa-up" } ?: return
        intent.data = null
        val url = data.getQueryParameter("url").orEmpty()
        val token = data.getQueryParameter("token").orEmpty()
        if (!Sync.validUrl(url) || token.isEmpty()) {
            Toast.makeText(this, "Invalid pairing link", Toast.LENGTH_LONG).show()
            return
        }
        AlertDialog.Builder(this)
            .setTitle("Pair with this server?")
            .setMessage(Uri.parse(url).host)
            .setPositiveButton("Pair") { _, _ ->
                urlInput.setText(url)
                tokenInput.setText(token)
                saveServer(url, token)
            }
            .setNegativeButton("Cancel", null)
            .show()
    }

    private fun saveServer(url: String, token: String) {
        if (!Sync.validUrl(url) || token.isEmpty()) {
            Toast.makeText(this, "Enter an https:// URL and a token", Toast.LENGTH_LONG).show()
            return
        }
        preferences.edit().putString(Sync.URL_KEY, url).putString(Sync.TOKEN_KEY, token).apply()
        Sync.schedule(this, force = true)
        Toast.makeText(this, "Saved", Toast.LENGTH_SHORT).show()
        showStatus()
    }

    private fun requestRegistration() {
        if (Registration.register(this)) {
            showStatus()
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
                showStatus()
            }
        } catch (e: RuntimeException) {
            Toast.makeText(this, "Could not select a push distributor", Toast.LENGTH_LONG).show()
        }
    }

    private fun showStatus() {
        val endpoint = preferences.getString(Wake.ENDPOINT_KEY, null)
        val distributor = runCatching { UnifiedPush.getSavedDistributor(this) }.getOrNull()
        val available = runCatching {
            distributor != null && distributor in UnifiedPush.getDistributors(this)
        }.getOrDefault(false)
        statusView.text = when {
            distributor == null -> "Choose a push distributor"
            !available -> "Distributor unavailable"
            endpoint == null -> "Waiting for registration"
            else -> "Registered with distributor"
        }
        syncView.text = if (preferences.getString(Sync.URL_KEY, null) == null) {
            "Not paired: scan the server's QR code or enter its details"
        } else {
            preferences.getString(Sync.STATUS_KEY, null) ?: "Waiting to sync"
        }
    }
}
