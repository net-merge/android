package io.github.senseiredsmoke.netmerge.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color

/** Colours that carry meaning across the app: one per network type plus the state colours. */
@Immutable
data class Palette(
    val wifi: Color,
    val cell: Color,
    val good: Color,
    val warn: Color,
    val bad: Color,
    val backgroundTop: Color,
    val backgroundBottom: Color,
    val card: Color,
    val muted: Color,
)

private val DarkPalette = Palette(
    wifi = Color(0xFF38BDF8),
    cell = Color(0xFFFB923C),
    good = Color(0xFF34D399),
    warn = Color(0xFFFBBF24),
    bad = Color(0xFFF87171),
    backgroundTop = Color(0xFF0B1120),
    backgroundBottom = Color(0xFF111C33),
    card = Color(0xFF16223B),
    muted = Color(0xFF94A3B8),
)

private val LightPalette = Palette(
    wifi = Color(0xFF0284C7),
    cell = Color(0xFFEA580C),
    good = Color(0xFF059669),
    warn = Color(0xFFD97706),
    bad = Color(0xFFDC2626),
    backgroundTop = Color(0xFFF1F5FB),
    backgroundBottom = Color(0xFFE2EAF6),
    card = Color(0xFFFFFFFF),
    muted = Color(0xFF64748B),
)

val LocalPalette = staticCompositionLocalOf { DarkPalette }

@Composable
fun NetMergeTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    val palette = if (dark) DarkPalette else LightPalette
    val scheme = if (dark) {
        darkColorScheme(
            primary = palette.good,
            onPrimary = Color(0xFF052E22),
            background = palette.backgroundTop,
            surface = palette.card,
            onSurface = Color(0xFFE2E8F0),
            onBackground = Color(0xFFE2E8F0),
        )
    } else {
        lightColorScheme(
            primary = palette.good,
            onPrimary = Color.White,
            background = palette.backgroundTop,
            surface = palette.card,
            onSurface = Color(0xFF0F172A),
            onBackground = Color(0xFF0F172A),
        )
    }
    androidx.compose.runtime.CompositionLocalProvider(LocalPalette provides palette) {
        MaterialTheme(colorScheme = scheme, content = content)
    }
}
