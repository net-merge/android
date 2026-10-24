package org.netmerge.android

import android.Manifest
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import org.netmerge.android.ui.MainScreen
import org.netmerge.android.ui.NetMergeTheme

class MainActivity : ComponentActivity() {

    private val vpnConsent = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        if (result.resultCode == RESULT_OK) {
            MergeVpnService.start(this)
        } else {
            Toast.makeText(this, R.string.vpn_denied, Toast.LENGTH_LONG).show()
        }
    }

    private val notifPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        Engine.loadSettings(this)
        setContent {
            NetMergeTheme {
                val phase by Engine.phase.collectAsState()
                val stats by Engine.stats.collectAsState()
                val history by Engine.history.collectAsState()
                val even by Engine.evenSplit.collectAsState()
                MainScreen(
                    phase = phase,
                    stats = stats,
                    history = history,
                    evenSplit = even,
                    onToggle = ::toggle,
                    onEvenSplit = { Engine.setEvenSplit(this, it) },
                )
            }
        }
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            notifPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
        // Debug builds only: lets the CI screenshot job switch merging on without tapping.
        if (BuildConfig.DEBUG && intent.getBooleanExtra("autostart", false) && Engine.phase.value == Engine.Phase.OFF) {
            toggle()
        }
    }

    private fun toggle() {
        if (Engine.phase.value != Engine.Phase.OFF) {
            MergeVpnService.stop(this)
            return
        }
        val consent = VpnService.prepare(this)
        if (consent != null) vpnConsent.launch(consent) else MergeVpnService.start(this)
    }
}
