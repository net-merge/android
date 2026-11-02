package org.netmerge.android

import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * App-wide state shared by the VPN service, the UI and the Quick Settings tile. The service
 * samples the Go engine once a second; the UI only observes.
 */
object Engine {
    /** PAUSED: the service runs and watches the networks, but nothing is captured (one network works). */
    enum class Phase { OFF, STARTING, ON, PAUSED }

    /** Seconds of throughput history kept for the live chart. */
    const val HISTORY = 60

    private val _phase = MutableStateFlow(Phase.OFF)
    val phase: StateFlow<Phase> = _phase.asStateFlow()

    private val _stats = MutableStateFlow<List<Stats>>(emptyList())
    val stats: StateFlow<List<Stats>> = _stats.asStateFlow()

    /** Per network: total (down + up) bytes/s for the last [HISTORY] seconds, oldest first. */
    private val _history = MutableStateFlow<Map<String, List<Float>>>(emptyMap())
    val history: StateFlow<Map<String, List<Float>>> = _history.asStateFlow()

    private val _evenSplit = MutableStateFlow(false)
    val evenSplit: StateFlow<Boolean> = _evenSplit.asStateFlow()

    private val _useAllNetworks = MutableStateFlow(true)
    val useAllNetworks: StateFlow<Boolean> = _useAllNetworks.asStateFlow()

    private val _autoPause = MutableStateFlow(true)
    val autoPause: StateFlow<Boolean> = _autoPause.asStateFlow()

    private val _startOnBoot = MutableStateFlow(false)
    val startOnBoot: StateFlow<Boolean> = _startOnBoot.asStateFlow()

    /** The newest release's tag, once GitHub has one newer than this app. */
    private val _update = MutableStateFlow<String?>(null)
    val update: StateFlow<String?> = _update.asStateFlow()

    val running: Boolean get() = _phase.value == Phase.ON

    internal fun setUpdate(tag: String) {
        _update.value = tag
    }

    internal fun setPhase(p: Phase) {
        _phase.value = p
        if (p == Phase.OFF) {
            _stats.value = emptyList()
            _history.value = emptyMap()
        }
    }

    /** Reads the engine's counters. Called by the service every second while it runs. */
    internal fun sample() {
        val now = Stats.parse(netmerge.Netmerge.stats()).filter { it.present }
        _stats.value = now
        val old = _history.value
        _history.value = now.associate { s ->
            val prev = old[s.name] ?: List(HISTORY) { 0f }
            s.name to (prev + (s.rateIn + s.rateOut).toFloat()).takeLast(HISTORY)
        }
    }

    // --- settings ---

    private fun prefs(ctx: Context) = ctx.getSharedPreferences("settings", Context.MODE_PRIVATE)

    fun loadSettings(ctx: Context) {
        val p = prefs(ctx)
        _evenSplit.value = p.getBoolean("even_split", false)
        _useAllNetworks.value = p.getBoolean("use_all_networks", true)
        _autoPause.value = p.getBoolean("auto_pause", true)
        _startOnBoot.value = p.getBoolean("start_on_boot", false)
        runCatching { netmerge.Netmerge.setLimitsJSON(p.getString("limits", "{}")) }
        applySettings()
        runCatching { netmerge.Netmerge.autoSetEnabled(_autoPause.value, running) }
    }

    fun setEvenSplit(ctx: Context, on: Boolean) {
        prefs(ctx).edit().putBoolean("even_split", on).apply()
        _evenSplit.value = on
        applySettings()
    }

    fun setUseAllNetworks(ctx: Context, on: Boolean) {
        prefs(ctx).edit().putBoolean("use_all_networks", on).apply()
        _useAllNetworks.value = on
        applySettings()
    }

    fun setAutoPause(ctx: Context, on: Boolean) {
        prefs(ctx).edit().putBoolean("auto_pause", on).apply()
        _autoPause.value = on
        runCatching { netmerge.Netmerge.autoSetEnabled(on, running) }
    }

    fun setStartOnBoot(ctx: Context, on: Boolean) {
        prefs(ctx).edit().putBoolean("start_on_boot", on).apply()
        _startOnBoot.value = on
    }

    fun wantsStartOnBoot(ctx: Context): Boolean = prefs(ctx).getBoolean("start_on_boot", false)

    /** Caps a network in Mbit/s (0 removes the cap) and remembers it. */
    fun setLimit(ctx: Context, name: String, mbps: Double) {
        runCatching {
            netmerge.Netmerge.setLimit(name, mbps)
            prefs(ctx).edit().putString("limits", netmerge.Netmerge.limitsJSON()).apply()
        }
    }

    private fun applySettings() {
        runCatching { netmerge.Netmerge.setAutoWeights(!_evenSplit.value) }
        runCatching { netmerge.Netmerge.setIPv6Mode(if (_useAllNetworks.value) "auto" else "on") }
    }
}
