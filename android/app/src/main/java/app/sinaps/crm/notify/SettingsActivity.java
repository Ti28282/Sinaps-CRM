package app.sinaps.crm.notify;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ActivityNotFoundException;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.os.PowerManager;
import android.provider.Settings;
import android.view.Gravity;
import android.view.ViewGroup;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

/** Настройки приложения: с какой CRM связан телефон, работа в фоне, отвязка. */
public class SettingsActivity extends Activity {
    @Override protected void onResume() {
        super.onResume();
        build();
    }

    private void build() {
        LinearLayout root = Ui.col(this);
        root.setPadding(Ui.dp(this, 16), Ui.dp(this, 14), Ui.dp(this, 16), Ui.dp(this, 16));

        LinearLayout head = Ui.row(this);
        TextView back = Ui.text(this, "‹", 26, Ui.TEXT, false);
        back.setGravity(Gravity.CENTER);
        back.setContentDescription("Назад");
        back.setBackground(Ui.box(this, Ui.CARD, Ui.LINE, 12));
        back.setOnClickListener(v -> finish());
        head.addView(back, new LinearLayout.LayoutParams(Ui.dp(this, 48), Ui.dp(this, 48)));
        TextView h = Ui.text(this, "Настройки", 22, Ui.TEXT, true);
        h.setPadding(Ui.dp(this, 14), 0, 0, 0);
        head.addView(h);
        root.addView(head);

        boolean on = LinkService.state == LinkService.ONLINE;
        StringBuilder addr = new StringBuilder();
        for (String s : Cfg.hosts(this)) addr.append(addr.length() == 0 ? "" : ", ").append(s);
        LinearLayout card = Ui.col(this);
        card.setPadding(Ui.dp(this, 16), Ui.dp(this, 14), Ui.dp(this, 16), Ui.dp(this, 14));
        card.setBackground(Ui.box(this, Ui.CARD, Ui.LINE, 18));
        card.addView(Ui.text(this, Cfg.crm(this), 16, Ui.TEXT, true));
        card.addView(Ui.text(this, on ? "На связи" : "Нет связи: компьютер выключен или телефон в другой сети", 13, on ? Ui.ACCENT : Ui.MUTED, false));
        card.addView(Ui.text(this, "Адрес компьютера: " + addr + " · порт " + Cfg.port(this), 12, Ui.MUTED, false),
                Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 8));
        root.addView(card, Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 18));

        PowerManager pm = getSystemService(PowerManager.class);
        boolean free = pm != null && pm.isIgnoringBatteryOptimizations(getPackageName());
        TextView bat = Ui.button(this, free ? "Работа в фоне разрешена ✓" : "Разрешить работу в фоне", free ? 1 : 0);
        bat.setOnClickListener(v -> open(new Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:" + getPackageName())),
                new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)));
        root.addView(bat, Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 14));
        root.addView(Ui.text(this, "Без этого телефон может усыпить приложение, и уведомления начнут опаздывать.", 12, Ui.MUTED, false),
                Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 6));

        TextView sound = Ui.button(this, "Звук и вид уведомлений", 1);
        sound.setOnClickListener(v -> open(new Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_APP_PACKAGE, getPackageName()), null));
        root.addView(sound, Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 14));

        TextView readAll = Ui.button(this, "Отметить всё прочитанным", 1);
        readAll.setOnClickListener(v -> { Feed.get(this).markAllRead(); finish(); });
        root.addView(readAll, Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 8));

        TextView unpair = Ui.button(this, "Отвязать телефон", 2);
        unpair.setOnClickListener(v -> new AlertDialog.Builder(this)
                .setTitle("Отвязать телефон?")
                .setMessage("Уведомления перестанут приходить, лента на телефоне очистится. В CRM тоже нажми «Отвязать» в «Плагины → Уведомления».")
                .setPositiveButton("Отвязать", (d, w) -> {
                    stopService(new Intent(this, LinkService.class));
                    Cfg.clear(this);
                    Feed.get(this).clear();
                    startActivity(new Intent(this, PairActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK));
                    finish();
                })
                .setNegativeButton("Отмена", null)
                .show());
        root.addView(unpair, Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 24));

        root.addView(Ui.text(this, "Приложение только показывает сообщения. Клиентам оно ничего не отправляет. Что присылать и что видно в уведомлении — настраивается в CRM.", 12, Ui.MUTED, false),
                Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 18));

        ScrollView scroll = new ScrollView(this);
        scroll.setBackgroundColor(Ui.BG);
        scroll.addView(root);
        setContentView(scroll);
    }

    private void open(Intent first, Intent fallback) {
        try {
            startActivity(first);
        } catch (ActivityNotFoundException | SecurityException e) {
            if (fallback != null) {
                try { startActivity(fallback); } catch (ActivityNotFoundException | SecurityException ignored) {}
            }
        }
    }
}
