package app.sinaps.crm.notify;

import android.Manifest;
import android.app.Activity;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.PackageManager;
import android.os.Build;
import android.os.Bundle;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.BaseAdapter;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.ListView;
import android.widget.TextView;

import java.util.ArrayList;
import java.util.List;

/** Лента уведомлений: что прислала CRM. */
public class MainActivity extends Activity {
    private static final String[][] FILTERS = {{"all", "Все"}, {"chat", "Сообщения"}, {"event", "События CRM"}};

    private String filter = "all";
    private List<Ev> rows = new ArrayList<>();
    private final Rows adapter = new Rows();
    private TextView status, empty;
    private final TextView[] tabs = new TextView[FILTERS.length];
    private final BroadcastReceiver changed = new BroadcastReceiver() {
        @Override public void onReceive(Context c, Intent i) { refresh(); }
    };

    @Override protected void onCreate(Bundle b) {
        super.onCreate(b);
        if (!Cfg.paired(this)) {
            startActivity(new Intent(this, PairActivity.class));
            finish();
            return;
        }
        LinkService.start(this);
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, 1);
        }

        LinearLayout root = Ui.col(this);
        root.setBackgroundColor(Ui.BG);
        root.setPadding(Ui.dp(this, 16), Ui.dp(this, 14), Ui.dp(this, 16), 0);

        LinearLayout head = Ui.row(this);
        head.addView(Ui.text(this, "Уведомления", 24, Ui.TEXT, true), Ui.lp(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        status = Ui.text(this, "", 12, Ui.MUTED, false);
        status.setPadding(Ui.dp(this, 10), Ui.dp(this, 6), Ui.dp(this, 10), Ui.dp(this, 6));
        head.addView(status);
        head.addView(Ui.space(this, 8));
        TextView gear = Ui.text(this, "⚙", 20, Ui.TEXT, false);
        gear.setGravity(Gravity.CENTER);
        gear.setContentDescription("Настройки");
        gear.setBackground(Ui.box(this, Ui.CARD, Ui.LINE, 12));
        gear.setOnClickListener(v -> startActivity(new Intent(this, SettingsActivity.class)));
        head.addView(gear, new LinearLayout.LayoutParams(Ui.dp(this, 48), Ui.dp(this, 48)));
        root.addView(head);

        LinearLayout tabRow = Ui.row(this);
        for (int i = 0; i < FILTERS.length; i++) {
            final String id = FILTERS[i][0];
            TextView t = Ui.text(this, FILTERS[i][1], 13, Ui.TEXT, false);
            t.setGravity(Gravity.CENTER);
            t.setMinHeight(Ui.dp(this, 44));
            t.setPadding(Ui.dp(this, 14), 0, Ui.dp(this, 14), 0);
            t.setOnClickListener(v -> { filter = id; refresh(); });
            LinearLayout.LayoutParams p = Ui.lp(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0);
            p.rightMargin = Ui.dp(this, 6);
            tabRow.addView(t, p);
            tabs[i] = t;
        }
        root.addView(tabRow, Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 14));

        FrameLayout body = new FrameLayout(this);
        ListView list = new ListView(this);
        list.setDivider(null);
        list.setSelector(android.R.color.transparent);
        list.setAdapter(adapter);
        list.setClipToPadding(false);
        list.setPadding(0, Ui.dp(this, 10), 0, Ui.dp(this, 16));
        list.setOnItemClickListener((p, v, pos, id) -> open(rows.get(pos)));
        body.addView(list);
        empty = Ui.text(this, "Пока пусто.\nКогда клиент напишет, сообщение появится здесь.", 14, Ui.MUTED, false);
        empty.setGravity(Gravity.CENTER);
        body.addView(empty, new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
        root.addView(body, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1));

        setContentView(root);
    }

    @Override protected void onResume() {
        super.onResume();
        if (!Cfg.paired(this)) { finish(); return; }
        IntentFilter f = new IntentFilter(LinkService.CHANGED);
        if (Build.VERSION.SDK_INT >= 33) registerReceiver(changed, f, Context.RECEIVER_NOT_EXPORTED);
        else registerReceiver(changed, f);
        refresh();
    }

    @Override protected void onPause() {
        try { unregisterReceiver(changed); } catch (IllegalArgumentException ignored) {}
        super.onPause();
    }

    private void open(Ev e) {
        if (e.chatId != 0 && !Feed.get(this).thread(e.chatId).isEmpty()) {
            startActivity(new Intent(this, ThreadActivity.class).putExtra("chatId", e.chatId));
        } else {
            Feed.get(this).markRead(e.seq);
            refresh();
        }
    }

    private void refresh() {
        if (status == null) return;
        boolean on = LinkService.state == LinkService.ONLINE;
        status.setText(on ? "● CRM на связи" : "● нет связи");
        status.setTextColor(on ? Ui.ACCENT : Ui.MUTED);
        status.setBackground(Ui.box(this, Ui.CARD, Ui.LINE, 999));
        for (int i = 0; i < tabs.length; i++) {
            boolean sel = FILTERS[i][0].equals(filter);
            tabs[i].setTextColor(sel ? Ui.INK : 0xFFC9CCD2);
            tabs[i].setBackground(Ui.box(this, sel ? Ui.TEXT : 0, sel ? 0 : 0xFF2A2D35, 999));
            tabs[i].setSelected(sel);
        }
        rows = Feed.get(this).list(filter);
        adapter.notifyDataSetChanged();
        empty.setVisibility(rows.isEmpty() ? View.VISIBLE : View.GONE);
    }

    private final class Rows extends BaseAdapter {
        @Override public int getCount() { return rows.size(); }
        @Override public Object getItem(int i) { return rows.get(i); }
        @Override public long getItemId(int i) { return rows.get(i).seq; }

        @Override public View getView(int i, View old, ViewGroup parent) {
            Context c = MainActivity.this;
            Ev e = rows.get(i);
            LinearLayout wrap = Ui.col(c);
            wrap.setPadding(0, 0, 0, Ui.dp(c, 8));
            LinearLayout card = Ui.row(c);
            card.setGravity(Gravity.TOP);
            card.setPadding(Ui.dp(c, 14), Ui.dp(c, 14), Ui.dp(c, 14), Ui.dp(c, 14));
            card.setBackground(Ui.box(c, Ui.CARD, e.read ? Ui.LINE : 0xFF3A4420, 18));

            if (e.isChat() && !e.client.isEmpty()) {
                card.addView(Ui.avatar(c, e.client, e.chatId, 42));
            } else {
                TextView ic = Ui.text(c, "alarm".equals(e.kind) ? "!" : "S", 16, "alarm".equals(e.kind) ? Ui.DANGER : Ui.ACCENT, true);
                ic.setGravity(Gravity.CENTER);
                ic.setBackground(Ui.box(c, 0xFF1E2026, Ui.LINE, 14));
                card.addView(ic, new LinearLayout.LayoutParams(Ui.dp(c, 42), Ui.dp(c, 42)));
            }

            LinearLayout mid = Ui.col(c);
            mid.setPadding(Ui.dp(c, 12), 0, 0, 0);
            LinearLayout top = Ui.row(c);
            TextView title = Ui.text(c, e.title, 15, Ui.TEXT, !e.read);
            Ui.ellipsize(title, 1);
            top.addView(title, Ui.lp(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
            TextView time = Ui.text(c, Ui.when(e.at), 12, Ui.MUTED, false);
            time.setPadding(Ui.dp(c, 8), 0, 0, 0);
            top.addView(time);
            mid.addView(top);
            if (!e.text.isEmpty()) {
                TextView text = Ui.text(c, e.text, 14, e.read ? Ui.MUTED : 0xFFC9CCD2, false);
                Ui.ellipsize(text, 2);
                mid.addView(text, Ui.top(c, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 4));
            }
            String ch = Ui.channel(e.channel);
            if (!ch.isEmpty()) {
                TextView badge = Ui.text(c, ch + (e.stage.isEmpty() ? "" : " · этап «" + e.stage + "»"), 12, "wa".equals(e.channel) ? Ui.WA : Ui.TG, false);
                mid.addView(badge, Ui.top(c, Ui.lp(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 6));
            }
            card.addView(mid, Ui.lp(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
            wrap.addView(card);
            return wrap;
        }
    }
}
