package io.github.senseiredsmoke.netmerge.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke

/**
 * Stacked area chart of the last minute, one band per network, scrolling smoothly to the left as
 * each new one-second sample arrives.
 */
@Composable
fun LiveChart(series: List<Pair<Color, List<Float>>>, modifier: Modifier = Modifier) {
    // Slide one step per sample so the chart moves continuously instead of jumping.
    val shift = remember { Animatable(0f) }
    val stamp = series.sumOf { it.second.lastOrNull()?.toDouble() ?: 0.0 } + series.size
    LaunchedEffect(stamp) {
        shift.snapTo(1f)
        shift.animateTo(0f, tween(1000, easing = LinearEasing))
    }
    val grid = LocalPalette.current.muted.copy(alpha = 0.15f)

    Canvas(modifier) {
        val n = series.maxOfOrNull { it.second.size } ?: 0
        for (i in 1..3) {
            val y = size.height * i / 4f
            drawLine(grid, Offset(0f, y), Offset(size.width, y))
        }
        if (n < 2) return@Canvas
        // Running totals, so each band sits on top of the previous one.
        val stacked = ArrayList<FloatArray>()
        var below = FloatArray(n)
        for ((_, values) in series) {
            val top = FloatArray(n) { i -> below[i] + values.getOrElse(i - (n - values.size)) { 0f } }
            stacked += top
            below = top
        }
        val peak = (below.maxOrNull() ?: 0f).coerceAtLeast(1f) * 1.15f
        val step = size.width / (n - 2)
        // Newest sample on the right edge; the oldest one sits just off the left edge so the
        // chart can slide in from the right without a gap.
        fun x(i: Int) = size.width + (i - (n - 1) + shift.value) * step
        fun y(v: Float) = size.height - v / peak * size.height

        var lower = FloatArray(n)
        series.forEachIndexed { k, (color, _) ->
            val upper = stacked[k]
            val area = Path()
            area.moveTo(x(0), y(upper[0]))
            for (i in 1 until n) smooth(area, x(i - 1), y(upper[i - 1]), x(i), y(upper[i]))
            area.lineTo(x(n - 1), y(lower[n - 1]))
            for (i in n - 2 downTo 0) smooth(area, x(i + 1), y(lower[i + 1]), x(i), y(lower[i]))
            area.close()
            drawPath(area, Brush.verticalGradient(listOf(color.copy(alpha = 0.55f), color.copy(alpha = 0.12f))))
            val line = Path()
            line.moveTo(x(0), y(upper[0]))
            for (i in 1 until n) smooth(line, x(i - 1), y(upper[i - 1]), x(i), y(upper[i]))
            drawPath(line, color, style = Stroke(width = 2.5f * density))
            lower = upper
        }
    }
}

private fun smooth(path: Path, x0: Float, y0: Float, x1: Float, y1: Float) {
    val mid = (x0 + x1) / 2
    path.cubicTo(mid, y0, mid, y1, x1, y1)
}
