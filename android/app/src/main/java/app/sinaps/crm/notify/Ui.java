package app.sinaps.crm.notify;

import android.content.Context;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.text.TextUtils;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.View;
import android.widget.LinearLayout;
import android.widget.TextView;

import java.text.SimpleDateFormat;
import java.util.Calendar;
import java.util.Date;
import java.util.Locale;

/** Тёмная тема приложения и мелкие помощники для экранов (вёрстка в коде, без xml). */
final class Ui {
    static final int BG = 0xFF0D0E11, CARD = 0xFF16181D, LINE = 0xFF23262D, TEXT = 0xFFECEDEF,
            MUTED = 0xFF8A8F98, ACCENT = 0xFFC8F25A, INK = 0xFF0D0E11, DANGER = 0xFFF28B82,
            TG = 0xFF2AABEE, WA = 0xFF25D366;
    private static final int[] AVATARS = {0xFFF5B65C, 0xFFB39DFF, 0xFF7CC6F2, 0xFFC8F25A, 0xFFF59BB0, 0xFF7FE0AE};

    private Ui() {}

    static int dp(Context c, float v) {
        return Math.round(TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, v, c.getResources().getDisplayMetrics()));
    }

    static GradientDrawable box(Context c, int fill, int stroke, float radiusDp) {
        GradientDrawable g = new GradientDrawable();
        g.setColor(fill);
        g.setCornerRadius(dp(c, radiusDp));
        if (stroke != 0) g.setStroke(dp(c, 1), stroke);
        return g;
    }

    static TextView text(Context c, String s, float sp, int color, boolean bold) {
        TextView t = new TextView(c);
        t.setText(s);
        t.setTextSize(TypedValue.COMPLEX_UNIT_SP, sp);
        t.setTextColor(color);
        if (bold) t.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        return t;
    }

    /** Кнопка высотой 52dp. kind: 0 — главная (акцент), 1 — обычная, 2 — опасная. */
    static TextView button(Context c, String s, int kind) {
        TextView t = text(c, s, 15, kind == 0 ? INK : kind == 2 ? DANGER : TEXT, true);
        t.setGravity(Gravity.CENTER);
        t.setMinHeight(dp(c, 52));
        t.setPadding(dp(c, 18), dp(c, 8), dp(c, 18), dp(c, 8));
        t.setBackground(box(c, kind == 0 ? ACCENT : CARD, kind == 0 ? 0 : LINE, 14));
        t.setClickable(true);
        t.setFocusable(true);
        return t;
    }

    static TextView avatar(Context c, String title, long id, int sizeDp) {
        TextView t = text(c, initials(title), sizeDp * 0.36f, INK, true);
        t.setGravity(Gravity.CENTER);
        GradientDrawable g = new GradientDrawable();
        g.setShape(GradientDrawable.OVAL);
        g.setColor(AVATARS[(int) (Math.abs(id) % AVATARS.length)]);
        t.setBackground(g);
        t.setLayoutParams(new LinearLayout.LayoutParams(dp(c, sizeDp), dp(c, sizeDp)));
        return t;
    }

    static String initials(String title) {
        StringBuilder b = new StringBuilder();
        for (String w : title.replaceAll("[«»\"']", "").trim().split("\\s+")) {
            if (!w.isEmpty() && b.length() < 2) b.append(w.substring(0, w.offsetByCodePoints(0, 1)));
        }
        return b.length() == 0 ? "?" : b.toString().toUpperCase(Locale.ROOT);
    }

    static LinearLayout col(Context c) {
        LinearLayout l = new LinearLayout(c);
        l.setOrientation(LinearLayout.VERTICAL);
        return l;
    }

    static LinearLayout row(Context c) {
        LinearLayout l = new LinearLayout(c);
        l.setOrientation(LinearLayout.HORIZONTAL);
        l.setGravity(Gravity.CENTER_VERTICAL);
        return l;
    }

    static LinearLayout.LayoutParams lp(int w, int h, float weight) {
        return new LinearLayout.LayoutParams(w, h, weight);
    }

    static LinearLayout.LayoutParams top(Context c, LinearLayout.LayoutParams p, float dp) {
        p.topMargin = dp(c, dp);
        return p;
    }

    static View space(Context c, float dp) {
        View v = new View(c);
        v.setLayoutParams(new LinearLayout.LayoutParams(dp(c, dp), dp(c, dp)));
        return v;
    }

    static void ellipsize(TextView t, int lines) {
        t.setMaxLines(lines);
        t.setEllipsize(TextUtils.TruncateAt.END);
    }

    static String channel(String ch) { return "wa".equals(ch) ? "WhatsApp" : "tg".equals(ch) ? "Telegram" : ""; }

    static String when(long unix) {
        if (unix <= 0) return "";
        Date d = new Date(unix * 1000);
        Calendar a = Calendar.getInstance(), b = Calendar.getInstance();
        a.setTime(d);
        if (a.get(Calendar.YEAR) == b.get(Calendar.YEAR) && a.get(Calendar.DAY_OF_YEAR) == b.get(Calendar.DAY_OF_YEAR)) {
            return new SimpleDateFormat("HH:mm", Locale.getDefault()).format(d);
        }
        b.add(Calendar.DAY_OF_YEAR, -1);
        if (a.get(Calendar.YEAR) == b.get(Calendar.YEAR) && a.get(Calendar.DAY_OF_YEAR) == b.get(Calendar.DAY_OF_YEAR)) return "вчера";
        return new SimpleDateFormat("d MMM", new Locale("ru")).format(d);
    }

    static String clock(long unix) {
        return unix <= 0 ? "" : new SimpleDateFormat("HH:mm", Locale.getDefault()).format(new Date(unix * 1000));
    }
}
