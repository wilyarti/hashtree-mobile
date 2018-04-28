package com.wordpress.clinetworking.hashtree_mobile
/*
import android.app.AlertDialog
import android.app.Dialog
import android.app.ProgressDialog
import org.jetbrains.anko.coroutines.experimental.bg
import org.jetbrains.anko.*
import android.os.Message
import android.media.audiofx.Equalizer
import android.preference.ListPreference
import android.preference.Preference
import android.preference.PreferenceActivity
import android.preference.PreferenceFragment
import android.preference.RingtonePreference
import kotlinx.coroutines.experimental.*
import android.content.DialogInterface
*/
import android.content.Intent
import android.content.Context
import android.widget.Toast
import android.view.View
import android.widget.*
import android.support.v7.app.AppCompatActivity
import android.os.Bundle
import android.os.Environment
import android.os.Handler
import android.preference.PreferenceManager
import android.widget.TextView
import hashfunc.Hashfunc
import kotlinx.android.synthetic.main.activity_main.*
import kotlinx.coroutines.experimental.android.UI
import kotlinx.coroutines.experimental.async


import java.io.*

class MainActivity : AppCompatActivity() {
    // make categories global
    val categories = ArrayList<String>()
    // get external dir
    var extStore = Environment.getExternalStorageDirectory();
    var w = extStore.toString() + "/Workspace"

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        // spinner
        val spinner: Spinner = findViewById(R.id.spinner)
        categories.add("PRESS LIST")
        val dataAdapter = ArrayAdapter(this, android.R.layout.simple_spinner_item, categories)
        spinner.adapter = dataAdapter
        //// end

        // callback
        val handler: Handler.Callback

        button.setOnClickListener {
            val intent = Intent(this, SettingsActivity::class.java)
            startActivity(intent)
        }
        button2.setOnClickListener {
            //toast("Fetching snapshot list.")
            //list()
        }
        button3.setOnClickListener {
            button3.setEnabled(false)
            //async(UI) { test() }
            // variables
            var server = PreferenceManager.getDefaultSharedPreferences(this).getString("text_server", "")
            var accesskey = PreferenceManager.getDefaultSharedPreferences(this).getString("text_accesskey", "")
            var secretkey = PreferenceManager.getDefaultSharedPreferences(this).getString("text_secretkey", "")
            var enckey = PreferenceManager.getDefaultSharedPreferences(this).getString("text_enckey", "")
            var bucket = PreferenceManager.getDefaultSharedPreferences(this).getString("text_bucket", "")
            var secure = PreferenceManager.getDefaultSharedPreferences(this).getBoolean("example_switch", true)
            var snapshot_name: String
            snapshot_name = spinner.selectedItem.toString()
            val fileName: String = w + "/" + snapshot_name
            textView.append("Starting download of $snapshot_name\n")
            Download(server, accesskey, secretkey, enckey, bucket, snapshot_name, fileName, secure, w)
        }
        button4.setOnClickListener {
            button4.setEnabled(false)
            //async(UI) { test() }
            // variables
            var server = PreferenceManager.getDefaultSharedPreferences(this).getString("text_server", "")
            var accesskey = PreferenceManager.getDefaultSharedPreferences(this).getString("text_accesskey", "")
            var secretkey = PreferenceManager.getDefaultSharedPreferences(this).getString("text_secretkey", "")
            var enckey = PreferenceManager.getDefaultSharedPreferences(this).getString("text_enckey", "")
            var bucket = PreferenceManager.getDefaultSharedPreferences(this).getString("text_bucket", "")
            var secure = PreferenceManager.getDefaultSharedPreferences(this).getBoolean("example_switch", true)
            var snapshot_name: String
            snapshot_name = spinner.selectedItem.toString()
            val fileName: String = w + "/" + snapshot_name
            textView.append("Starting download of $snapshot_name\n")
            Upload(server, accesskey, secretkey, enckey, bucket, snapshot_name, fileName, secure, w)

        }
        spinner.onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
            override fun onNothingSelected(parent: AdapterView<*>?) {}
            override fun onItemSelected(parent: AdapterView<*>?, view: View?, position: Int, id: Long) {}
        }

    }

    // creates toast messages to be displayed
    fun Context.toast(message: CharSequence) =
            Toast.makeText(this, message, Toast.LENGTH_SHORT).show()

    // needed to get spinner to select from generated list
    private fun getTaskType(spinner: Spinner): String {

        var tasks = "no text"

        // Spinner click listener
        spinner.onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
            override fun onItemSelected(parent: AdapterView<*>, view: View, position: Int, id: Long) {
                tasks = parent.getItemAtPosition(position).toString()
            }

            override fun onNothingSelected(parent: AdapterView<*>) {
                //do nothing
            }
        }

        return tasks
    }

    fun Context.test() {
        fun sleeper(i: Int): String {
            Thread.sleep(100)
            return "foo $i\n"
        }
        for (i in 0..100) {
            var f = sleeper(i)
            textView.append(f)
        }
        button3.setEnabled(true)
    }

    fun Context.Download(server: String, accesskey: String, secretkey: String, enckey: String, bucket: String, snapshot_name: String, fileName: String, secure: Boolean, w: String) = async(UI) {
        // text view console
        val tview: TextView = findViewById(R.id.textView)
        tview.append("Fetching snapshotlist...\n")
        var s = Hashfunc.download(server, 443, secure, accesskey, secretkey, enckey, fileName, snapshot_name, bucket, true)
        if (s != "ERROR") {
            var remotedb = mutableMapOf<String, MutableList<String>>()
            remotedb = readdb(fileName)
            println("Count " + remotedb.count())
            for ((key, value) in remotedb) {
                val iterate = value.listIterator()
                while (iterate.hasNext()) {
                    var i = 0
                    var fpath = w + "/" + iterate.next()
                    var targetFile = File(fpath);
                    var parent = targetFile.getParentFile();
                    if (!parent.exists() && !parent.mkdirs()) {
                        throw IllegalStateException("Couldn't create dir: " + parent);
                        toast("Couldn't create dir for $fpath.")
                    } else {
                        while (i < 3) {
                            var s = Hashfunc.download(server, 443, secure, accesskey, secretkey, enckey, fpath, key, bucket, true)
                            if (s == "ERROR") {
                                i++
                            } else {
                                tview.append("[D] => fpath")
                                i = 3
                            }
                        }
                    }
                }
            }
        } else {
            toast("Error downloading snapshot!")
        }
        button3.setEnabled(true)
    }

    fun Context.list() {
        var server = PreferenceManager.getDefaultSharedPreferences(this).getString("text_server", "")
        var accesskey = PreferenceManager.getDefaultSharedPreferences(this).getString("text_accesskey", "")
        var secretkey = PreferenceManager.getDefaultSharedPreferences(this).getString("text_secretkey", "")
        var enckey = PreferenceManager.getDefaultSharedPreferences(this).getString("text_enckey", "")
        var secure = PreferenceManager.getDefaultSharedPreferences(this).getBoolean("example_switch", true)
        var bucket = PreferenceManager.getDefaultSharedPreferences(this).getString("text_bucket", "")

        var h = Hashfunc.hashlist(server, secure, accesskey, secretkey, bucket)

        if (h != "ERROR") {
            var snapshots = h.lines()
            snapshots.forEach {
                var fpath = w + "/" + it
                if (!it.isBlank()) {
                    categories.add(it)
                    textView.append(it + "\n")
                }
            }
        }
    }

    fun Context.readdb(fileName: String): MutableMap<String, MutableList<String>> {
// using extension function readLines
        var hash = ""
        var fpath = ""
        var remotedb = mutableMapOf<String, MutableList<String>>()
        var dec = 0
        try {
            File(fileName).readLines().forEach {
                var regex = "^--- .*".toRegex()
                var regex1 = "^---".toRegex()
                var regex2 = "^- .*".toRegex()

                if (regex.matches(it)) {
                    val re = "^--- ".toRegex()
                    hash = re.replace(it, "")
                    // reset decrement
                    dec = 0
                } else if (regex1.matches(it)) {
                    // set decrement
                    dec = 1;
                }
                // only processs if decremented
                if (dec == 1) {
                    if (regex2.matches(it)) {
                        // remove formatting
                        val re = "^- ".toRegex()
                        fpath = re.replace(it, "")
                        // append to map
                        remotedb[hash] = mutableListOf()
                        remotedb[hash]?.add(fpath)
                        //textView.append(hash + " => " + fpath)
                    }

                }
            }
            return remotedb
        } catch (t: Throwable) {
            textView.append(t.toString())
        }
        return remotedb
    }

    fun Context.Upload(server: String, accesskey: String, secretkey: String, enckey: String, bucket: String, snapshot_name: String, fileName: String, secure: Boolean, w: String) = async(UI) {
        // text view console
        val tview: TextView = findViewById(R.id.textView)
        tview.append("Uploading files...\n")
        //var s = Hello.upload(server, 443, secure, accesskey, secretkey, enckey, fileName, snapshot_name, bucket, true)
        val i = Hashfunc.hashtree(server, accesskey, secretkey, enckey, bucket, secure, w)
        if (i) {
            toast("Uploads successful!")
        } else {
            toast("Failed to upload!")
        }
        button4.setEnabled(true)
    }
}